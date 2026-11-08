package desktop

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferenceImportValidatesRealPixelsBeforeReturning(t *testing.T) {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 6)))
	if ref, err := makeReference("测试.png", b.Bytes()); err != nil || !strings.HasPrefix(ref.DataURL, "data:image/png;base64,") || ref.ID == "" {
		t.Fatalf("reference: %v %v", ref.Name, err)
	}
	if _, err := makeReference("truncated.png", b.Bytes()[:20]); err == nil {
		t.Fatal("truncated image accepted")
	}
	if _, err := makeReference("oversize.png", make([]byte, maxReferenceBytes+1)); err == nil {
		t.Fatal("oversize accepted")
	}
}
func TestImportsIgnoreFilesAfterFourSlots(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "image.png")
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	_ = os.WriteFile(file, b.Bytes(), 0600)
	d := New(nil, root)
	refs, err := d.ImportFiles([]string{file, file, file, file, filepath.Join(root, "does-not-exist")}, 4)
	if err != nil || len(refs) != 4 {
		t.Fatalf("refs=%d err=%v", len(refs), err)
	}
	if _, err = d.ImportFiles([]string{root}, 1); err == nil {
		t.Fatal("directory accepted as a reference")
	}
	refs, err = d.ImportFiles([]string{file, filepath.Join(root, "ignored-invalid")}, 1)
	if err != nil || len(refs) != 1 {
		t.Fatalf("remaining slot read ignored file: %v", err)
	}
}
