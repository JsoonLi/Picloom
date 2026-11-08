package application

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

func TestReadThumbnailGeneratesOnceAndCachesOnDisk(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := image.NewNRGBA(image.Rect(0, 0, 1200, 900))
	for i := range source.Pix {
		source.Pix[i] = 0xff
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "image-gen", "images")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "large.png")
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	row := domain.ImageRecord{ID: "large", ImagePath: path, ReferencePaths: []string{}}
	if err := store.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = []domain.ImageRecord{row}; return nil }); err != nil {
		t.Fatal(err)
	}
	backend := New(store)
	first, err := backend.ReadThumbnail(row.ID, 312, 960)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(first))
	if err != nil || format != "jpeg" || config.Width != 312 || config.Height != 234 {
		t.Fatalf("opaque thumbnail must be a 312x234 JPEG: %+v %s %v", config, format, err)
	}
	// The original is gone: a second read must come from the disk cache.
	if err := os.WriteFile(path, []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if cached, err := backend.ReadThumbnail(row.ID, 312, 960); err != nil || !bytes.Equal(cached, first) {
		t.Fatalf("thumbnail must be served from the disk cache: %v", err)
	}
	if err := store.Delete(row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadThumbnail(row.ID, 312, 960); !os.IsNotExist(err) {
		t.Fatalf("deleting an artwork must remove its cached thumbnails: %v", err)
	}
	if _, err := backend.ReadThumbnail(row.ID, 0, 960); err == nil {
		t.Fatal("invalid thumbnail bounds must be rejected")
	}
}

func TestReadThumbnailReturnsSmallOriginalsUnchanged(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	small := image.NewNRGBA(image.Rect(0, 0, 40, 30))
	small.SetNRGBA(0, 0, color.NRGBA{1, 2, 3, 4})
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, small)
	dir := filepath.Join(root, "image-gen", "images")
	_ = os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, "small.png")
	_ = os.WriteFile(path, encoded.Bytes(), 0600)
	if err := store.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		*rows = []domain.ImageRecord{{ID: "small", ImagePath: path, ReferencePaths: []string{}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := New(store).ReadThumbnail("small", 312, 960)
	if err != nil || !bytes.Equal(data, encoded.Bytes()) {
		t.Fatalf("small originals must not be re-encoded: %v", err)
	}
}
