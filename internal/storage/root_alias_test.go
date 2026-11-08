package storage

import (
	"bytes"
	"image2ai/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyImagesThroughTrustedDataRootLink(t *testing.T) {
	actual := t.TempDir()
	alias := filepath.Join(t.TempDir(), "Image2AI")
	if err := os.Symlink(actual, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	record := fixture("legacy-alias", alias)
	if err := os.MkdirAll(filepath.Dir(record.ImagePath), 0700); err != nil {
		t.Fatal(err)
	}
	pixels := []byte("synthetic-image-bytes")
	if err := os.WriteFile(record.ImagePath, pixels, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = append(*rows, record); return nil }); err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadImage(record.ID)
	if err != nil || !bytes.Equal(got, pixels) {
		t.Fatalf("legacy root link unreadable: %v", err)
	}
	if err = store.Delete(record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(record.ImagePath); !os.IsNotExist(err) {
		t.Fatal("owned alias image not deleted")
	}
}
