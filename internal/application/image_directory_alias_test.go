package application

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

func TestChosenDirectoryAliasRoundTrip(t *testing.T) {
	target := t.TempDir()
	alias := filepath.Join(t.TempDir(), "chosen")
	if linkErr := os.Symlink(target, alias); linkErr != nil && runtime.GOOS == "windows" {
		output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", alias, target).CombinedOutput()
		if err != nil {
			t.Skipf("junction unavailable: %v %s", err, output)
		}
	} else if linkErr != nil {
		t.Skip(linkErr)
	}
	pixels, err := os.ReadFile(filepath.Join("..", "..", "build", "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(pixels)}}})
	}))
	defer server.Close()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend := New(store)
	if _, err = backend.SaveProviders([]domain.Provider{{ID: "local", Name: "local", BaseURL: server.URL, Models: []string{"m"}, Resolutions: []string{"2k"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = backend.SaveProxy(domain.ProxyConfig{Mode: "direct"}); err != nil {
		t.Fatal(err)
	}
	if _, err = backend.SaveImageDirectory(alias); err != nil {
		t.Fatal(err)
	}
	resolved, resolveErr := filepath.EvalSymlinks(alias)
	t.Logf("alias=%q target=%q canonical=%q err=%v", alias, target, resolved, resolveErr)
	record, err := backend.Generate(context.Background(), domain.GenerateArgs{Prompt: "alias round trip", ProviderID: "local", Model: "m", Size: "1024x1024"}, nil)
	if err != nil {
		filepath.WalkDir(target, func(path string, _ os.DirEntry, walkErr error) error {
			t.Logf("target entry %s: %v", path, walkErr)
			return nil
		})
		t.Fatal(err)
	}
	t.Logf("chosen=%s canonical saved=%s", alias, record.ImagePath)
	if _, err = backend.ReadImage(record.ID); err != nil {
		t.Fatalf("newly generated image cannot be read: %v", err)
	}
	if config, err := backend.LoadConfig(); err != nil || config.ImageDirectory != resolved {
		t.Fatalf("chosen directory must be canonical: %+v %v", config, err)
	}
	// Older configurations may have stored the alias as a current or previous location.
	for _, previous := range []bool{false, true} {
		if _, err := store.UpdateConfig(func(c *domain.Config) error {
			c.ImageDirectory = alias
			c.PreviousImageDirectories = nil
			if previous {
				c.ImageDirectory = ""
				c.PreviousImageDirectories = []string{alias}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := backend.ReadImage(record.ID); err != nil {
			t.Fatalf("legacy alias configuration cannot read saved work: previous=%v %v", previous, err)
		}
	}
	if err := backend.Delete(record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(record.ImagePath); !os.IsNotExist(err) {
		t.Fatalf("deleting work must remove its canonical file: %v", err)
	}
}
