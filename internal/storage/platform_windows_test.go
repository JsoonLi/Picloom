//go:build windows

package storage

import (
	"bytes"
	"image2ai/internal/domain"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWindowsTransientReplacementIsBounded(t *testing.T) {
	s := openTest(t)
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "dark"; return nil }); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(s.Root(), "config.json")
	before, _ := os.ReadFile(destination)
	calls := 0
	s.replace = func(source, target string) error {
		calls++
		if calls <= 2 {
			return syscall.Errno(32)
		}
		return replaceFile(source, target)
	}
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "light"; return nil }); err != nil || calls != 3 {
		t.Fatalf("transient retry failed: %d %v", calls, err)
	}
	updated, _ := os.ReadFile(destination)
	if bytes.Equal(updated, before) {
		t.Fatal("successful retry did not replace data")
	}
	calls = 0
	s.replace = func(source, target string) error { calls++; return syscall.Errno(32) }
	if _, err := s.UpdateConfig(func(c *domain.Config) error { c.Theme = "dark"; return nil }); err == nil || calls != 6 {
		t.Fatalf("retry was not bounded: %d %v", calls, err)
	}
	after, _ := os.ReadFile(destination)
	if !bytes.Equal(after, updated) {
		t.Fatal("permanent sharing lock changed destination")
	}
}

func TestWindowsImageDirectoryJunctionCannotEscape(t *testing.T) {
	s := openTest(t)
	images := filepath.Join(s.Root(), "image-gen", "images")
	outside := t.TempDir()
	os.MkdirAll(filepath.Dir(images), 0700)
	data := []byte("keep outside")
	os.WriteFile(filepath.Join(outside, "outside.png"), data, 0600)
	if out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", images, outside).CombinedOutput(); err != nil {
		t.Fatalf("test junction setup: %v %s", err, out)
	}
	row := fixture("outside", s.Root())
	if err := s.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = append(*rows, row); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadImage(row.ID); err == nil {
		t.Fatal("directory junction escaped data root")
	}
	if err := s.Delete(row.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(outside, "outside.png"))
	if !bytes.Equal(after, data) {
		t.Fatal("outside junction target changed")
	}
}
