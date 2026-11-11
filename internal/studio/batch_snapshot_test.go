package studio

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
)

// TestBatchPanelSnapshots renders the batch panel when
// IMAGE2AI_STUDIO_SNAPSHOTS names a folder, for checking its layout.
func colorPNG(w, h int, r, g, b uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{r + uint8(x*60/w), g + uint8(y*60/h), b, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestBatchPanelSnapshots(t *testing.T) {
	output := os.Getenv("IMAGE2AI_STUDIO_SNAPSHOTS")
	if output == "" {
		t.Skip("set IMAGE2AI_STUDIO_SNAPSHOTS to capture rendered UI")
	}
	for _, size := range [][2]int{{1280, 900}, {740, 560}} {
		for _, dark := range []bool{false, true} {
			a, tt, base, d := fixture(t, size[0], size[1])
			backend := &batchBackend{testBackend: base, release: make(chan struct{})}
			desktop := &batchDesktop{testDesktop: d, files: map[string][]byte{}}
			a.backend, a.desktop = backend, desktop
			tt.SetDark(dark)
			var paths []string
			for i, name := range []string{"cat01.png", "cat02.jpg", "portrait.png", "panorama.png"} {
				path := filepath.Join("photos", name)
				paths = append(paths, path)
				desktop.files[path] = colorPNG([]int{400, 300, 300, 640}[i], []int{300, 400, 300, 270}[i], uint8(60*i), 120, 180)
			}
			paths = append(paths, filepath.Join("photos", "broken.png"))
			a.openBatch()
			a.addBatchFiles(func() ([]string, error) { return paths, nil })
			settle(a, tt)
			b := a.batch
			b.prompt = "把背景换成纯白，保留主体"
			b.shared = []domain.Reference{{ID: "style", Name: "style.png", DataURL: pngURL(colorPNG(64, 64, 200, 80, 60))}}
			b.sharedThumbs["style"], _ = thumbnail(colorPNG(64, 64, 200, 80, 60), referenceThumbSize, referenceThumbSize)
			b.outputDir = "/Users/me/out/batch"
			b.items[0].expanded = true
			settle(a, tt)
			theme := map[bool]string{false: "light", true: "dark"}[dark]
			file, err := os.Create(filepath.Join(output, "batch-"+theme+"-"+fmtSize(size)+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
			file.Close()
		}
	}
}
