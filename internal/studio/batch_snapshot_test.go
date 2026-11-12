package studio

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"image2ai/internal/domain"
	"image2ai/internal/locale"
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
	t.Cleanup(func() { locale.Set(locale.Chinese) })
	for _, size := range [][2]int{{1280, 900}, {740, 560}} {
		for _, dark := range []bool{false, true} {
			a, tt, base, d := fixture(t, size[0], size[1])
			backend := &batchBackend{testBackend: base, release: make(chan struct{})}
			desktop := &batchDesktop{testDesktop: d, files: map[string][]byte{}}
			a.backend, a.desktop = backend, desktop
			tt.SetDark(dark)
			theme := map[bool]string{false: "light", true: "dark"}[dark]
			save := func(name string) {
				t.Helper()
				for _, language := range []string{locale.Chinese, locale.English} {
					locale.Set(language)
					for i := 0; i < 5; i++ {
						a.drain()
						tt.Frame()
					}
					file, err := os.Create(filepath.Join(output, name+"-"+language+"-"+theme+"-"+fmtSize(size)+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, tt.Image())
					file.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				locale.Set(locale.Chinese)
			}
			var paths []string
			for i, name := range []string{"cat01.png", "cat02.jpg", "portrait.png", "panorama.png"} {
				path := filepath.Join("photos", name)
				paths = append(paths, path)
				desktop.files[path] = colorPNG([]int{400, 300, 300, 640}[i], []int{300, 400, 300, 270}[i], uint8(60*i), 120, 180)
			}
			paths = append(paths, filepath.Join("photos", "broken.png"))
			a.openBatch()
			save("batch-empty")
			a.addBatchFiles(func() ([]string, error) { return paths, nil })
			settle(a, tt)
			b := a.batch
			b.prompt = "把背景换成纯白，保留主体"
			b.shared = []domain.Reference{{ID: "style", Name: "style.png", DataURL: pngURL(colorPNG(64, 64, 200, 80, 60))}}
			b.sharedThumbs["style"], _ = thumbnail(colorPNG(64, 64, 200, 80, 60), referenceThumbSize, referenceThumbSize)
			b.outputDir = "/Users/me/out/batch"
			b.items[0].expanded = true
			settle(a, tt)
			save("batch-draft")
			// Start it: two finish, one fails, the rest wait.
			a.startBatch()
			waitFor(t, a, func() bool { return !a.batchRuns[0].creating })
			run := a.batchRuns[0]
			backend.failName = pngURL(desktop.files[paths[1]])
			releaseOne(t, a, backend)
			releaseOne(t, a, backend)
			waitFor(t, a, func() bool { done, failed, _ := run.counts(); return done+failed >= 2 })
			releaseOne(t, a, backend)
			waitFor(t, a, func() bool { done, failed, _ := run.counts(); return done+failed >= 3 })
			// One request is still held, so wait for the screen, not for
			// every background task.
			frames := func() {
				for i := 0; i < 20; i++ {
					a.drain()
					tt.Frame()
					time.Sleep(5 * time.Millisecond)
				}
			}
			frames()
			save("batch-run")
			b.open = false
			a.refreshHistory()
			frames()
			save("batch-card")
			close(backend.release)
			waitFor(t, a, func() bool { return !run.running })
		}
	}
}
