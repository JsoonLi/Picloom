package application

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

// A complete IHDR is enough for DecodeConfig; no large pixel allocation is needed.
func oversizedPNGHeader() []byte {
	data := make([]byte, 33)
	copy(data, "\x89PNG\r\n\x1a\n")
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], 8193)
	binary.BigEndian.PutUint32(data[20:24], 8192)
	data[24], data[25] = 8, 6
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func TestHistoryImageReadsRejectOversizedHeadersBeforeDecode(t *testing.T) {
	header := oversizedPNGHeader()
	config, _, err := image.DecodeConfig(bytes.NewReader(header))
	if err != nil || config.Width != 8193 || config.Height != 8192 {
		t.Fatalf("fixture header is not decodable: %+v %v", config, err)
	}
	var valid bytes.Buffer
	if err := png.Encode(&valid, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		data      []byte
		wantError string
	}{
		{"oversized", header, "像素尺寸"},
		{"invalid-format", []byte("not an image"), "无法解码"},
		{"valid", valid.Bytes(), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := storage.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			paths := make([]string, 2)
			for i, bucket := range []string{"images", "references"} {
				dir := filepath.Join(root, "image-gen", bucket)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				paths[i] = filepath.Join(dir, "fixture.png")
				if err := os.WriteFile(paths[i], test.data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			row := domain.ImageRecord{ID: "legacy", ImagePath: paths[0], ReferencePaths: []string{paths[1]}, ReferenceCount: 1}
			if err := store.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = []domain.ImageRecord{row}; return nil }); err != nil {
				t.Fatal(err)
			}
			// Storage preserves legacy bytes. The application must validate before
			// passing them to native thumbnail, preview or clipboard decoders.
			if got, err := store.ReadImage(row.ID); err != nil || !bytes.Equal(got, test.data) {
				t.Fatalf("legacy fixture unreadable: %v", err)
			}
			backend := New(store)
			for _, read := range []func() ([]byte, error){
				func() ([]byte, error) { return backend.ReadImage(row.ID) },
				func() ([]byte, error) { return backend.ReadReference(row.ID, 0) },
			} {
				data, err := read()
				if test.wantError == "" {
					if err != nil || !bytes.Equal(data, test.data) {
						t.Fatalf("valid image changed: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), test.wantError) || data != nil {
					t.Fatalf("unsafe image reached decoder: returned %d bytes, error %v; want %q", len(data), err, test.wantError)
				}
			}
		})
	}
}
