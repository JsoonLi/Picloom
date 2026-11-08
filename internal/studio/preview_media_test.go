package studio

import (
	"bytes"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

// The same local artwork is immutable across theme and viewport captures.
var previewMediaArtwork = sync.OnceValue(func() [][]byte {
	return [][]byte{visualArtwork(0), visualArtwork(1), visualArtwork(2), visualArtwork(3), visualArtwork(4)}
})

type previewMediaBackend struct {
	*testBackend
	references [][]byte
	gates      map[int]<-chan struct{}
}

func (b *previewMediaBackend) ReadReference(_ string, index int) ([]byte, error) {
	if gate := b.gates[index]; gate != nil {
		<-gate
	}
	return b.references[index], nil
}

func TestPreviewResultAndReferenceThumbnailSelection(t *testing.T) {
	for _, width := range []int{1280, 375} {
		for _, dark := range []bool{false, true} {
			name := fmt.Sprintf("%d-%v", width, dark)
			t.Run(name, func(t *testing.T) {
				a, tt, b, _ := fixture(t, width, 900)
				tt.SetDark(dark)
				result := previewMediaArtwork()[0]
				b.image = result
				refs := previewMediaArtwork()[1:]
				a.backend = &previewMediaBackend{testBackend: b, references: refs}
				record := domain.ImageRecord{ID: "comparison", Prompt: "对照结果图和参考图", ProviderName: "本机模拟服务", Model: "studio-image", Size: "640x480", CreatedAt: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC).UnixMilli(), MIMEType: "image/png", Width: 640, Height: 480, ReferenceCount: len(refs)}
				a.openArtwork(record)
				settle(a, tt)
				if width < 900 {
					box, _ := tt.Find("图片缩放查看器")
					tt.Scroll(box.X+10, box.Y+box.H+20, 0, 2000)
					tt.Frame()
				}
				for _, label := range []string{"结果图", "参考图 1", "参考图 2", "参考图 3", "参考图 4"} {
					box, ok := tt.Find(label)
					if !ok || box.X < 0 || box.Y < 0 || box.X+box.W > float32(width) || box.Y+box.H > 900 {
						t.Fatalf("thumbnail %s is unreachable: %v", label, box)
					}
				}
				if !bytes.Equal(a.previewData, result) || a.previewMedia.selected != 0 || !tt.HasText("✓") {
					t.Fatal("result must be selected initially")
				}
				resultBox, _ := tt.Find("结果图")
				x, y := int(resultBox.X+resultBox.W/2), int(resultBox.Y+1)
				selectedBorder := tt.Image().RGBAAt(x, y)
				if output := os.Getenv("IMAGE2AI_PREVIEW_MEDIA_SNAPSHOTS"); output != "" {
					writeMarkupSnapshot(t, tt, output, "result-"+name)
				}
				thumb := a.previewMedia.result
				session := a.previewMedia.session
				a.viewer.fitted, a.viewer.x = false, 30
				click(t, tt, "参考图 4")
				settle(a, tt)
				if a.previewMedia.selected != 4 || !bytes.Equal(a.previewData, refs[3]) || !a.viewer.fitted || a.viewer.x != 0 {
					t.Fatal("reference click must replace the full image and reset zoom/pan")
				}
				if a.previewMedia.session != session || a.previewMedia.result != thumb || len(a.previewMedia.references) != 4 || a.previewTitle != "作品详情" {
					t.Fatal("switching must preserve the thumbnail strip and dialog")
				}
				if tt.Image().RGBAAt(x, y) == selectedBorder || !tt.HasText("✓") {
					t.Fatal("selected thumbnail highlight did not move")
				}
				if output := os.Getenv("IMAGE2AI_PREVIEW_MEDIA_SNAPSHOTS"); output != "" {
					writeMarkupSnapshot(t, tt, output, "reference-"+name)
				}
				click(t, tt, "结果图")
				settle(a, tt)
				if a.previewMedia.selected != 0 || !bytes.Equal(a.previewData, result) {
					t.Fatal("result thumbnail must restore the result image")
				}
				a.previewOpen = false
				tt.Frame()
				if a.previewMedia.result != nil || a.previewMedia.references != nil || a.previewMedia.loading || a.previewBitmap != nil {
					t.Fatal("closing must release full images and thumbnail state")
				}
			})
		}
	}
}

func waitPreviewState(t *testing.T, a *App, tt *ui.Tester, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ready() && time.Now().Before(deadline) {
		a.drain()
		tt.Frame()
		time.Sleep(time.Millisecond)
	}
	if !ready() {
		t.Fatal("preview state did not settle")
	}
}

func TestPreviewSwitchIgnoresStaleFullImagesAndKeepsThumbnailLoads(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	gate := make(chan struct{})
	defer func() {
		if gate != nil {
			close(gate)
		}
		a.workers.Wait()
	}()
	b.image = previewMediaArtwork()[0]
	refs := previewMediaArtwork()[1:3]
	a.backend = &previewMediaBackend{testBackend: b, references: refs, gates: map[int]<-chan struct{}{0: gate}}
	record := domain.ImageRecord{ID: "slow-reference", Prompt: "快速切换", ReferenceCount: 2}
	a.openArtwork(record)
	waitPreviewState(t, a, tt, func() bool { return !a.previewLoading && a.previewBitmap != nil })
	a.openSavedReference(record, 0)
	a.openSavedReference(record, 1)
	waitPreviewState(t, a, tt, func() bool { return !a.previewLoading && bytes.Equal(a.previewData, refs[1]) })
	close(gate)
	gate = nil
	settle(a, tt)
	if a.previewMedia.selected != 2 || !bytes.Equal(a.previewData, refs[1]) || a.previewMedia.references[0] == nil || a.previewMedia.references[1] == nil {
		t.Fatal("slow old selection replaced the new image or discarded thumbnails")
	}
}

func TestPreviewUnavailableReferenceDoesNotHideOtherImages(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	b.image = previewMediaArtwork()[0]
	a.backend = &previewMediaBackend{testBackend: b, references: [][]byte{[]byte("invalid image"), previewMediaArtwork()[2]}}
	record := domain.ImageRecord{ID: "broken-reference", Prompt: "部分图片无法读取", ReferenceCount: 2}
	a.openArtwork(record)
	settle(a, tt)
	if a.previewMedia.result == nil || a.previewMedia.problems[1] == "" || a.previewMedia.references[1] == nil {
		t.Fatal("one unavailable reference must not hide the result or other thumbnails")
	}
	click(t, tt, "参考图 1")
	settle(a, tt)
	if a.problem == "" {
		t.Fatal("unreadable full reference must show an error")
	}
	click(t, tt, "结果图")
	settle(a, tt)
	if a.problem != "" || !bytes.Equal(a.previewData, b.image) || a.previewMedia.selected != 0 {
		t.Fatal("result thumbnail must recover the viewer")
	}
}

func TestPreviewOldThumbnailSessionCannotChangeAnotherArtwork(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	gate := make(chan struct{})
	defer func() {
		if gate != nil {
			close(gate)
		}
		a.workers.Wait()
	}()
	b.image = previewMediaArtwork()[0]
	a.backend = &previewMediaBackend{testBackend: b, references: [][]byte{previewMediaArtwork()[1]}, gates: map[int]<-chan struct{}{0: gate}}
	a.openArtwork(domain.ImageRecord{ID: "old", Prompt: "旧作品", ReferenceCount: 1})
	waitPreviewState(t, a, tt, func() bool { return !a.previewLoading && a.previewBitmap != nil })
	a.openArtwork(domain.ImageRecord{ID: "new", Prompt: "新作品"})
	waitPreviewState(t, a, tt, func() bool { return !a.previewLoading && !a.previewMedia.loading })
	close(gate)
	gate = nil
	settle(a, tt)
	if a.previewRecord.ID != "new" || len(a.previewMedia.references) != 0 || len(a.previewMedia.problems) != 1 || a.previewMedia.selected != 0 {
		t.Fatal("old thumbnail callback changed the new artwork")
	}
	if _, ok := tt.Find("结果图"); !ok {
		t.Fatal("artwork without references must still show its result thumbnail")
	}
}
