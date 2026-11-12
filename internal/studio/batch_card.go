package studio

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/locale"
)

// libraryEntry is one card of the gallery: a single work or a whole batch.
type libraryEntry struct {
	record *domain.ImageRecord
	run    *batchRun
}

func (e libraryEntry) key() string {
	if e.run != nil {
		return "batch-" + e.run.key
	}
	return e.record.ID
}

func (e libraryEntry) prompt() string {
	if e.run != nil {
		return e.run.meta.Prompt
	}
	return e.record.Prompt
}

// libraryKey tells whether the cached cards still match the gallery.
type libraryKey struct {
	first    *domain.ImageRecord
	records  int
	runs     int
	filtered bool
}

func (a *App) currentLibraryKey() libraryKey {
	key := libraryKey{records: len(a.history.Items), runs: len(a.batchRuns), filtered: a.search != "" || a.favoritesOnly}
	if len(a.history.Items) > 0 {
		key.first = &a.history.Items[0]
	}
	return key
}

// libraryEntries groups the shown works by batch: each batch is one card,
// placed among the works, which keep their order, by when it started.
// While searching or showing favourites, a batch shows when one of its
// works matches.
func (a *App) libraryEntries() []libraryEntry {
	key := a.currentLibraryKey()
	if a.libraryCache != nil && a.libraryCacheKey == key {
		return a.libraryCache
	}
	runs := map[string]*batchRun{}
	for _, run := range a.batchRuns {
		if run.meta.ID != "" {
			runs[run.meta.ID] = run
		}
	}
	matched := map[*batchRun]bool{}
	var records []*domain.ImageRecord
	for i := range a.history.Items {
		record := &a.history.Items[i]
		if run := runs[record.BatchID]; run != nil {
			matched[run] = true
			continue
		}
		records = append(records, record)
	}
	var shown []*batchRun
	for _, run := range a.batchRuns {
		if !key.filtered || matched[run] {
			shown = append(shown, run)
		}
	}
	// batchRuns is newest first, as the works are.
	entries := make([]libraryEntry, 0, len(records)+len(shown))
	next := 0
	for _, record := range records {
		for next < len(shown) && shown[next].meta.CreatedAt >= record.CreatedAt {
			entries = append(entries, libraryEntry{run: shown[next]})
			next++
		}
		entries = append(entries, libraryEntry{record: record})
	}
	for ; next < len(shown); next++ {
		entries = append(entries, libraryEntry{run: shown[next]})
	}
	a.libraryCache, a.libraryCacheKey = entries, key
	return entries
}

// batchCard shows a batch as one card: up to four results, how far it got,
// and opens the batch when clicked.
func (a *App) batchCard(c *ui.Context, run *batchRun) {
	done, failed, _ := run.counts()
	imageWidth, imageHeight := float32(artworkMaxCardWidth-2*artworkCardPadding-2), float32(artworkMaxImageHeight)
	glassPane(c, "batch-card-"+run.key).Radius(12).Padding(artworkCardPadding).Gap(6).Width(artworkMaxCardWidth).MaxWidthPercent(100).Height(artworkCardHeight).Children(func() {
		var results []string
		for _, item := range run.items {
			if item.Status == batchDone && item.RecordID != "" {
				results = append(results, item.RecordID)
				if len(results) == 4 {
					break
				}
			}
		}
		open := ui.Box(c.Key("batch-cover-"+run.key)).Size(imageWidth, imageHeight).Radius(6).Clip().Background(c.Theme().Background).Label(locale.T("打开批量任务 ") + run.meta.Prompt).Focusable()
		open.Children(func() {
			switch {
			case len(results) == 0:
				text := locale.T("等待结果…")
				if !run.running && !run.creating {
					text = locale.T("没有成功的结果")
				}
				ui.Box(c).Fill().Center().Children(func() { ui.Text(c, text).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted) })
			case len(results) == 1:
				a.batchCover(c, results[0], imageWidth, imageHeight)
			case len(results) == 2:
				w := (imageWidth - 2) / 2
				ui.Row(c).Fill().Gap(2).Children(func() {
					a.batchCover(c, results[0], w, imageHeight)
					a.batchCover(c, results[1], w, imageHeight)
				})
			case len(results) == 3:
				// One large result beside two small ones.
				w, h := (imageWidth-2)/2, (imageHeight-2)/2
				ui.Row(c).Fill().Gap(2).Children(func() {
					a.batchCover(c, results[0], w, imageHeight)
					ui.Column(c).Gap(2).Children(func() {
						a.batchCover(c, results[1], w, h)
						a.batchCover(c, results[2], w, h)
					})
				})
			default:
				w, h := (imageWidth-2)/2, (imageHeight-2)/2
				ui.Column(c).Fill().Gap(2).Children(func() {
					for row := 0; row < 2; row++ {
						ui.Row(c).Gap(2).Children(func() {
							a.batchCover(c, results[row*2], w, h)
							a.batchCover(c, results[row*2+1], w, h)
						})
					}
				})
			}
			ui.Text(c.Key("batch-badge-"+run.key), fmt.Sprintf(locale.T("批量 · %d 张"), len(run.items))).Absolute().Top(6).Left(6).Padding(2, 8).Radius(6).FontSize(textSize(c, 11)).FontWeight(600).Background(ui.RGBA(20, 20, 20, 0.66)).TextColor(ui.Hex("#ffffff"))
		})
		if open.Clicked() {
			a.openBatchRun(run)
		}
		ui.Text(c, run.meta.Prompt).MaxLines(2).Height(36).FontSize(textSize(c, 13))
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Height(24).Children(func() {
			status := fmt.Sprintf(locale.T("%d/%d 完成"), done, len(run.items))
			if failed > 0 {
				status += fmt.Sprintf(locale.T(" · %d 失败"), failed)
			}
			color := c.Theme().TextMuted
			if failed > 0 && !run.running {
				color = c.Theme().Danger
			}
			if run.running || run.creating {
				ui.Progress(c.Key("batch-card-progress-"+run.key), float64(done+failed)/float64(max(1, len(run.items)))).Width(48).Label(locale.T("批量进度"))
				if run.paused {
					status = locale.T("已暂停 · ") + status
				}
			}
			ui.Text(c.Key("batch-card-status-"+run.key), status).SingleLine().Grow(1).MinWidth(0).Tooltip(status).FontSize(textSize(c, 11)).TextColor(color)
			if !run.running && !run.creating {
				iconButton(c, "batch-card-delete-"+run.key, locale.T("删除批量任务 ")+run.meta.Prompt, "trash").Size(24, 24).TextColor(c.Theme().TextMuted).OnClick(func() { a.confirmDeleteBatch(run) })
			}
		})
	})
}

func (a *App) batchCover(c *ui.Context, id string, w, h float32) {
	a.wantThumbnail(id)
	ui.Box(c).Size(w, h).Clip().Children(func() {
		if thumb := a.thumbs[id]; thumb != nil {
			ui.Image(c.Key("batch-cover-image-"+id), thumb).Fill().Fit(ui.Cover)
		}
	})
}

func (a *App) confirmDeleteBatch(run *batchRun) {
	b := a.batchState()
	b.deleteKey, b.deleteOpen = run.key, true
}

func batchTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04")
}
