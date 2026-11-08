package studio

import (
	"runtime"
	"sort"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/generation"
)

const (
	// Bound thumbnail decoding while retaining detail for portrait crops.
	artworkThumbWidth  = 2 * (artworkMaxCardWidth - 2*artworkCardPadding - 2)
	artworkThumbHeight = 960
	referenceThumbSize = 2 * 64
	// thumbCacheBytes bounds the decoded artwork thumbnails kept in memory,
	// several screens of a large gallery.
	thumbCacheBytes = 64 << 20
)

// thumbLoadSlots bounds concurrent thumbnail loads. Cached thumbnails are
// small files; a first load decodes the whole original once.
func thumbLoadSlots() int { return max(2, min(4, runtime.NumCPU()/2)) }

// thumbnail decodes data into a bitmap no larger than maxW×maxH.
func thumbnail(data []byte, maxW, maxH int) (*ui.Bitmap, error) {
	img, err := generation.Thumbnail(data, maxW, maxH)
	if err != nil {
		return nil, err
	}
	return ui.NewBitmap(img), nil
}

// beginThumbnailFrame starts collecting the artworks the grid builds this
// frame: those on screen and the few rows around them.
func (a *App) beginThumbnailFrame() {
	a.thumbFrame++
	a.thumbWanted = a.thumbWanted[:0]
}

// wantThumbnail marks an artwork as built this frame, so its thumbnail is
// kept, or loaded when missing.
func (a *App) wantThumbnail(id string) {
	a.thumbUsed[id] = a.thumbFrame
	if a.thumbs[id] == nil && !a.thumbLoading[id] && a.thumbErrors[id] == "" {
		a.thumbWanted = append(a.thumbWanted, id)
	}
}

// pumpThumbnails starts loads for this frame's missing thumbnails, in grid
// order, up to the concurrency bound. Each finished load redraws the
// window, which pumps the next, so artworks scrolled past never load.
func (a *App) pumpThumbnails() {
	for _, id := range a.thumbWanted {
		if a.thumbInFlight >= thumbLoadSlots() {
			return
		}
		if !a.thumbLoading[id] {
			a.loadThumbnail(id)
		}
	}
}

func (a *App) loadThumbnail(id string) {
	a.thumbLoading[id] = true
	a.thumbInFlight++
	a.run(func() func() {
		data, err := a.backend.ReadThumbnail(id, artworkThumbWidth, artworkThumbHeight)
		var img *ui.Bitmap
		if err == nil {
			// DecodeBitmap turns JPEG originals upright as their EXIF says.
			img, err = ui.DecodeBitmap(data)
		}
		return func() {
			a.thumbInFlight--
			delete(a.thumbLoading, id)
			if err != nil {
				a.thumbErrors[id] = err.Error()
				return
			}
			a.cacheThumb(id, img)
			a.trimThumbs()
		}
	})
}

func thumbSize(bitmap *ui.Bitmap) int {
	w, h := bitmap.Size()
	return 4 * w * h
}

// cacheThumb stores an artwork thumbnail as just used.
func (a *App) cacheThumb(id string, bitmap *ui.Bitmap) {
	if old := a.thumbs[id]; old != nil {
		a.thumbBytes -= thumbSize(old)
	}
	a.thumbs[id] = bitmap
	a.thumbBytes += thumbSize(bitmap)
	a.thumbUsed[id] = a.thumbFrame
}

// trimThumbs evicts the least recently shown thumbnails beyond the memory
// budget, never those built in the latest frame.
func (a *App) trimThumbs() {
	if a.thumbBytes <= thumbCacheBytes {
		return
	}
	ids := make([]string, 0, len(a.thumbs))
	for id := range a.thumbs {
		if a.thumbUsed[id] != a.thumbFrame {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return a.thumbUsed[ids[i]] < a.thumbUsed[ids[j]] })
	for _, id := range ids {
		if a.thumbBytes <= thumbCacheBytes {
			break
		}
		a.dropThumb(id)
	}
}

func (a *App) dropThumb(id string) {
	if old := a.thumbs[id]; old != nil {
		a.thumbBytes -= thumbSize(old)
	}
	delete(a.thumbs, id)
	delete(a.thumbUsed, id)
}

func (a *App) forgetThumb(id string) {
	a.dropThumb(id)
	delete(a.thumbErrors, id)
}
