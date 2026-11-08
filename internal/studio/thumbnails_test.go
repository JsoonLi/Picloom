package studio

import (
	"fmt"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

func TestThumbCacheStaysWithinBudgetAndKeepsVisible(t *testing.T) {
	a := New(nil, nil)
	bitmap := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 512, 512))) // 1 MB
	count := thumbCacheBytes/(1<<20) + 8
	for i := range count {
		a.thumbFrame++
		a.cacheThumb(fmt.Sprint(i), bitmap)
	}
	a.thumbFrame++
	a.wantThumbnail("0") // the oldest is on screen again
	a.trimThumbs()
	if a.thumbBytes > thumbCacheBytes || len(a.thumbs) != thumbCacheBytes/(1<<20) {
		t.Fatalf("cache must stay within its budget: %d bytes, %d thumbnails", a.thumbBytes, len(a.thumbs))
	}
	if a.thumbs["0"] == nil || a.thumbs[fmt.Sprint(count-1)] == nil {
		t.Fatal("visible and newest thumbnails must stay cached")
	}
	if a.thumbs["1"] != nil {
		t.Fatal("least recently shown thumbnail must be evicted")
	}
}

func TestLargeGalleryBuildsAndLoadsOnlyVisibleArtworks(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	b.mu.Lock()
	b.records = nil
	for i := range 500 {
		b.records = append(b.records, domain.ImageRecord{ID: fmt.Sprintf("bulk-%d", i), Prompt: fmt.Sprintf("批量作品 %d", i), Model: "studio-image", Width: 640, Height: 480, CreatedAt: time.Date(2026, 11, 1, 0, 0, i, 0, time.UTC).UnixMilli()})
	}
	b.thumbReads = 0
	b.mu.Unlock()
	a.refreshHistory()
	settle(a, tt)
	if len(a.history.Items) != 500 {
		t.Fatalf("gallery must hold every artwork in one list, got %d", len(a.history.Items))
	}
	built := 0
	for _, record := range a.history.Items {
		if _, ok := tt.Find("复用参数 " + record.ID); ok {
			built++
		}
	}
	b.mu.Lock()
	reads := b.thumbReads
	b.mu.Unlock()
	if built == 0 || built > 60 || reads > 60 {
		t.Fatalf("only the rows on screen may be built and loaded: built=%d reads=%d", built, reads)
	}
	if _, ok := tt.Find("复用参数 bulk-499"); ok {
		t.Fatal("the last artwork must not be built before it scrolls into view")
	}
	grid, _ := tt.Find("作品和生成任务")
	for range 200 {
		tt.Scroll(grid.X+grid.W/2, grid.Y+grid.H/2, 0, 4000)
		tt.Frame()
	}
	settle(a, tt)
	if _, ok := tt.Find("复用参数 bulk-499"); !ok {
		t.Fatal("scrolling the grid must reach the last artwork")
	}
	if a.thumbs["bulk-499"] == nil {
		t.Fatal("the artwork scrolled to must load its thumbnail")
	}
	b.mu.Lock()
	reads = b.thumbReads
	b.mu.Unlock()
	if reads >= 500 {
		t.Fatalf("artworks scrolled past must not all load: reads=%d", reads)
	}
}

func TestPreviewStartsFromThumbnailAndReleasesOnClose(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	seedVisualGallery(a, tt, b)
	record := a.history.Items[0]
	thumb := a.thumbs[record.ID]
	if thumb == nil || record.Width == 0 {
		t.Skip("gallery fixture has no thumbnail with a recorded size")
	}
	a.openArtwork(record)
	if a.previewBitmap != thumb || a.previewWidth != float32(record.Width) {
		t.Fatal("preview must show the cached thumbnail at the original size while decoding")
	}
	settle(a, tt)
	if a.previewBitmap == thumb || a.previewWidth != 0 {
		t.Fatal("full image must replace the placeholder")
	}
	a.previewOpen = false
	tt.Frame()
	if a.previewBitmap != nil || a.previewLoading {
		t.Fatal("closing the preview must release the full image")
	}
}
