package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image2ai/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func fixture(id string, root string) domain.ImageRecord {
	return domain.ImageRecord{ID: id, Prompt: "合成图片", ProviderID: "local", ProviderName: "本地", Model: "fixture-image", Size: "1024x1024", CreatedAt: 1, ImagePath: filepath.Join(root, "image-gen", "images", id+".png"), MIMEType: "image/png", Width: 1, Height: 1, ReferencePaths: []string{}}
}

func TestLegacyConfigDefaultsAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	legacy := `{"providers":[{"id":"local","name":"本地","baseUrl":"http://127.0.0.1/v1","apiKey":"synthetic-only","models":["fixture-image"],"generationsPath":"/images/generations","editsPath":"/images/edits"}],"theme":"dark"}`
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Theme != "dark" || c.Proxy.Mode != "system" || c.GenerationPreferences.Ratio != "3:4" || len(c.Providers[0].Resolutions) != 2 || c.Providers[0].DefaultResolution != "2k" {
		t.Fatalf("legacy defaults: %+v", c)
	}
	c.Providers[0].Models[0] = "changed"
	fresh, _ := s.ReadConfig()
	if fresh.Providers[0].Models[0] != "fixture-image" {
		t.Fatal("read aliases internal state")
	}
	if _, err = s.UpdateConfig(func(c *domain.Config) error { c.Theme = "light"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fresh, err = s.ReadConfig()
	if err != nil || fresh.Theme != "light" || fresh.Providers[0].APIKey != "synthetic-only" {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestLegacy4KProviderGetsSupportedDefault(t *testing.T) {
	root := t.TempDir()
	legacy := `{"providers":[{"id":"local","name":"本地","baseUrl":"http://127.0.0.1","apiKey":"","models":["fixture-image"],"resolutions":["4k"],"generationsPath":"/images/generations","editsPath":"/images/edits"}]}`
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	config, err := s.ReadConfig()
	if err != nil || config.Providers[0].DefaultResolution != "4k" {
		t.Fatalf("legacy 4K default: %v", err)
	}
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "dark"; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptionNeverOverwritten(t *testing.T) {
	cases := []struct{ name, file, raw string }{
		{"config syntax", "config.json", "broken-json"},
		{"config nested missing", "config.json", `{"proxy":{"mode":"system","url":""}}`},
		{"config null field", "config.json", `{"theme":null}`},
		{"unsupported provider default", "config.json", `{"providers":[{"id":"local","name":"本地","baseUrl":"http://127.0.0.1","apiKey":"","models":["fixture-image"],"resolutions":["2k"],"defaultResolution":"4k","generationsPath":"/images/generations","editsPath":"/images/edits"}]}`},
		{"invalid provider default type", "config.json", `{"providers":[{"id":"local","name":"本地","baseUrl":"http://127.0.0.1","apiKey":"","models":["fixture-image"],"defaultResolution":null,"generationsPath":"/images/generations","editsPath":"/images/edits"}]}`},
		{"history missing fields", "image-gen/history.json", `[{"id":"bad"}]`},
		{"history null", "image-gen/history.json", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			f := filepath.Join(s.Root(), filepath.FromSlash(tc.file))
			os.MkdirAll(filepath.Dir(f), 0700)
			os.WriteFile(f, []byte(tc.raw), 0600)
			var err error
			if tc.file == "config.json" {
				_, err = s.UpdateConfig(func(c *domain.Config) error { c.Theme = "dark"; return nil })
			} else {
				err = s.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = append(*rows, fixture("new", s.Root())); return nil })
			}
			if err == nil {
				t.Fatal("invalid data accepted")
			}
			after, _ := os.ReadFile(f)
			if string(after) != tc.raw {
				t.Fatal("corruption overwritten")
			}
		})
	}
}

func TestTransactionsRetainConcurrentWrites(t *testing.T) {
	s := openTest(t)
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 40; i++ {
		wg.Go(func() {
			errs <- s.ChangeHistory(func(rows *[]domain.ImageRecord) error {
				*rows = append(*rows, fixture(fmt.Sprint(i), s.Root()))
				return nil
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ReadHistory()
	if err != nil || len(rows) != 40 {
		t.Fatalf("got %d records, %v", len(rows), err)
	}
	if err = s.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = nil; return errors.New("rollback") }); err == nil {
		t.Fatal("callback failure lost")
	}
	rows, _ = s.ReadHistory()
	if len(rows) != 40 {
		t.Fatal("callback error wrote data")
	}
}

func TestAtomicFailurePreservesDestination(t *testing.T) {
	s := openTest(t)
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "dark"; return nil }); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(s.Root(), "config.json")
	before, _ := os.ReadFile(file)
	calls := 0
	s.replace = func(a, b string) error { calls++; return errors.New("synthetic replacement failure") }
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "light"; return nil }); err == nil {
		t.Fatal("replacement succeeded unexpectedly")
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) || calls != 1 {
		t.Fatal("failed replacement changed destination")
	}
	entries, _ := os.ReadDir(s.Root())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatal("temporary file leaked")
		}
	}
}

func TestExclusiveDataRootLock(t *testing.T) {
	root := t.TempDir()
	a, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := Open(root); err == nil {
		b.Close()
		a.Close()
		t.Fatal("second writer acquired lock")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	if _, err := a.ReadConfig(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed store read: %v", err)
	}
}

func TestBackupOncePreservesRawLegacyFiles(t *testing.T) {
	s := openTest(t)
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "dark"; return nil }); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(s.Root(), "config.json"))
	if err := s.BackupLegacy(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "light"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.BackupLegacy(); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(s.Root(), "native-backup-v1", "config.json"))
	if err != nil || !bytes.Equal(before, backup) {
		t.Fatal("backup changed or not raw copy", err)
	}
}

func TestListFavoriteReadDeleteAndContainment(t *testing.T) {
	s := openTest(t)
	root := s.Root()
	images := filepath.Join(root, "image-gen", "images")
	refs := filepath.Join(root, "image-gen", "references")
	os.MkdirAll(images, 0700)
	os.MkdirAll(refs, 0700)
	a := fixture("a", root)
	a.CreatedAt = 2
	a.ReferencePaths = []string{filepath.Join(refs, "a-0.png")}
	a.ReferenceCount = 1
	b := fixture("b", root)
	b.Prompt = "另一幅"
	for _, f := range []string{a.ImagePath, b.ImagePath, a.ReferencePaths[0]} {
		if err := os.WriteFile(f, []byte("synthetic image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = []domain.ImageRecord{b, a}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Favorite("a", true); err != nil {
		t.Fatal(err)
	}
	p, err := s.List(domain.HistoryQuery{Page: 99, PageSize: 1, FavoritesOnly: true, Search: "合成"})
	if err != nil || p.Total != 1 || p.Page != 1 || p.Items[0].ID != "a" || p.FavoriteCount != 1 {
		t.Fatalf("list: %+v %v", p, err)
	}
	if data, err := s.ReadImage("a"); err != nil || string(data) != "synthetic image" {
		t.Fatal(err)
	}
	if _, err := s.ReadReference("a", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadReference("a", 4); err == nil {
		t.Fatal("invalid reference index allowed")
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	os.WriteFile(outside, []byte("keep"), 0600)
	escape := fixture("escape", root)
	escape.ImagePath = outside
	s.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = append(*rows, escape); return nil })
	if _, err := s.ReadImage("escape"); err == nil {
		t.Fatal("external image read")
	}
	if err := s.Delete("escape"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(outside); string(data) != "keep" {
		t.Fatal("external image deleted")
	}
	if err := s.Delete("a"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{a.ImagePath, a.ReferencePaths[0]} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Fatal("owned file not deleted", f)
		}
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	s := openTest(t)
	dir := filepath.Join(s.Root(), "image-gen", "images")
	os.MkdirAll(dir, 0700)
	outside := filepath.Join(t.TempDir(), "secret.png")
	os.WriteFile(outside, []byte("outside"), 0600)
	link := filepath.Join(dir, "escape.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip("OS did not permit test symlink")
	}
	row := fixture("escape", s.Root())
	row.ImagePath = link
	s.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = append(*rows, row); return nil })
	if _, err := s.ReadImage(row.ID); err == nil {
		t.Fatal("symlink escape read")
	}
	if err := s.Delete(row.ID); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(outside); string(data) != "outside" {
		t.Fatal("symlink target deleted")
	}
}

func TestInvalidMutationCannotWriteMissingOrMalformedData(t *testing.T) {
	s := openTest(t)
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Providers = nil; return nil }); err == nil {
		t.Fatal("invalid config committed")
	}
	if err := s.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		r := fixture("bad", s.Root())
		r.Width = -1
		*rows = append(*rows, r)
		return nil
	}); err == nil {
		t.Fatal("invalid history committed")
	}
	cfg, _ := s.ReadConfig()
	raw, _ := json.Marshal(cfg)
	if !bytes.Contains(raw, []byte(`"theme":"system"`)) {
		t.Fatal("default config damaged")
	}
}

func TestHistoryCacheFollowsWritesAndExternalChanges(t *testing.T) {
	s := openTest(t)
	if err := s.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		*rows = append(*rows, fixture("a", s.Root()))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ReadHistory()
	rows[0].Prompt = "changed by caller"
	if again, _ := s.ReadHistory(); again[0].Prompt == "changed by caller" {
		t.Fatal("reads must not alias the cached history")
	}
	if _, err := s.Favorite("a", true); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ReadHistory(); !rows[0].Favorite {
		t.Fatal("writes must refresh the cached history")
	}
	path := filepath.Join(s.Root(), "image-gen", "history.json")
	external, _ := json.Marshal([]domain.ImageRecord{fixture("a", s.Root()), fixture("b", s.Root())})
	if err := os.WriteFile(path, external, 0600); err != nil {
		t.Fatal(err)
	}
	if rows, err := s.ReadHistory(); err != nil || len(rows) != 2 {
		t.Fatalf("a changed history file must be read again: %d rows, %v", len(rows), err)
	}
	page, err := s.List(domain.HistoryQuery{All: true})
	if err != nil || len(page.Items) != 2 || page.TotalPages != 1 {
		t.Fatalf("All must return every record in one page: %+v %v", page, err)
	}
}
