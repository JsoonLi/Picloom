package application

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

func TestVectorAndGIFThumbnailsCacheRasterWithoutChangingOriginal(t *testing.T) {
	var animation bytes.Buffer
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}}
	frame := image.NewPaletted(image.Rect(0, 0, 4, 3), palette)
	frame.Pix[0] = 1
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{5, 5}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		format string
		data   []byte
	}{{"gif", animation.Bytes()}, {"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="30"><rect width="40" height="30" fill="red"/></svg>`)}} {
		t.Run(tc.format, func(t *testing.T) {
			root := t.TempDir()
			store, err := storage.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			directory := filepath.Join(root, "image-gen", "images")
			if err = os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "asset."+tc.format)
			if err = os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			if err = store.ChangeHistory(func(rows *[]domain.ImageRecord) error {
				*rows = []domain.ImageRecord{{ID: "asset", ImagePath: path, ReferencePaths: []string{}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			backend := New(store)
			data, err := backend.ReadThumbnail("asset", 312, 960)
			if err != nil {
				t.Fatal(err)
			}
			if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || (format != "png" && format != "jpeg") {
				t.Fatalf("thumbnail not a static raster: %s %v", format, err)
			}
			original, err := backend.ReadImage("asset")
			if err != nil || !bytes.Equal(original, tc.data) {
				t.Fatal("original changed")
			}
			if cached, err := store.ReadThumbnail("asset", 312, 960); err != nil || !bytes.Equal(cached, data) {
				t.Fatal("thumbnail not cached")
			}
		})
	}
}

func TestOutputFormatPreferencesRoundTrip(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend := New(store)
	preferences := domain.GenerationPreferences{Ratio: "3:4", Resolution: "2k", OutputFormat: "gif", GIFSize: "768x768", GIFFrameCount: 12, GIFFrameDurationMS: 100}
	if _, err = backend.SavePreferences(preferences); err != nil {
		t.Fatal(err)
	}
	config, err := backend.LoadConfig()
	if err != nil || config.GenerationPreferences != preferences {
		t.Fatal("format preference lost")
	}
	preferences.OutputFormat = "invalid"
	if _, err = backend.SavePreferences(preferences); err == nil {
		t.Fatal("invalid preference accepted")
	}
}
