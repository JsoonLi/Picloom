package desktop

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
)

type imageBackend struct {
	domain.Backend
	data []byte
}

func (b imageBackend) ReadImage(string) ([]byte, error) {
	if b.data == nil {
		return nil, errors.New("missing")
	}
	return b.data, nil
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCollectImagesKeepsImagesFromFilesAndFolders(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "folder")
	_ = os.MkdirAll(filepath.Join(folder, "nested"), 0700)
	for _, name := range []string{"b.png", "a.JPG", "notes.txt", filepath.Join("nested", "deep.png")} {
		_ = os.WriteFile(filepath.Join(folder, name), []byte("x"), 0600)
	}
	single := filepath.Join(root, "single.webp")
	_ = os.WriteFile(single, []byte("x"), 0600)
	got, err := New(nil, root).CollectImages([]string{single, folder, single, filepath.Join(folder, "b.png")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{single, filepath.Join(folder, "a.JPG"), filepath.Join(folder, "b.png")}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if _, err := New(nil, root).CollectImages([]string{filepath.Join(root, "missing.png")}); err == nil {
		t.Fatal("missing path accepted")
	}
}

func TestSaveBatchResultNeverReplacesFiles(t *testing.T) {
	dir := t.TempDir()
	data := pngBytes(t)
	d := New(imageBackend{data: data}, dir)
	existing := filepath.Join(dir, "cat.png")
	_ = os.WriteFile(existing, []byte("keep me"), 0600)
	first, err := d.SaveBatchResult("id", dir, "cat")
	if err != nil || first != filepath.Join(dir, "cat-1.png") {
		t.Fatalf("first %q %v", first, err)
	}
	second, err := d.SaveBatchResult("id", dir, "cat")
	if err != nil || second != filepath.Join(dir, "cat-2.png") {
		t.Fatalf("second %q %v", second, err)
	}
	if kept, _ := os.ReadFile(existing); string(kept) != "keep me" {
		t.Fatal("existing file replaced")
	}
	if written, _ := os.ReadFile(first); !bytes.Equal(written, data) {
		t.Fatal("result bytes changed")
	}
	if path, err := d.SaveBatchResult("id", dir, `a/b:c?`); err != nil || filepath.Dir(path) != dir {
		t.Fatalf("unsafe name escaped the folder: %q %v", path, err)
	}
	if _, err := d.SaveBatchResult("id", filepath.Join(dir, "missing"), "cat"); err == nil {
		t.Fatal("missing folder accepted")
	}
	if _, err := d.SaveBatchResult("id", "relative", "cat"); err == nil {
		t.Fatal("relative folder accepted")
	}
}
