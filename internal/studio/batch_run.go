package studio

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
	"image2ai/internal/locale"
)

// batchRun is one started batch. Its settings never change; its items move
// from queued to done, error or canceled, and each change is saved.
type batchRun struct {
	// key names the batch in the UI before the backend has given it an ID.
	key   string
	meta  domain.Batch
	items []*batchItem
	// shared holds the shared references while requests need them.
	shared        []domain.Reference
	sharedThumbs  []*ui.Bitmap
	sharedLoading bool
	creating      bool
	deleting      bool
	running       bool
	paused        bool
	active        int
	parallel      float64
	ctx           context.Context
	cancel        context.CancelFunc
	list          ui.ListState
}

func (r *batchRun) request() batchRequest {
	m := r.meta
	return batchRequest{batchID: m.ID, prompt: m.Prompt, providerID: m.ProviderID, model: m.Model, sizeMode: m.SizeMode, ratio: m.Ratio, resolution: m.Resolution, outputFormat: m.OutputFormat, shared: append([]domain.Reference(nil), r.shared...)}
}

func (r *batchRun) counts() (done, failed, waiting int) {
	for _, item := range r.items {
		switch item.Status {
		case batchDone:
			done++
		case batchFailed, batchCanceled:
			failed++
		default:
			waiting++
		}
	}
	return
}

func (item *batchItem) saved() domain.BatchItem {
	status := item.Status
	if status == batchGenerating {
		status = batchQueued
	}
	return domain.BatchItem{ID: item.ID, SourcePath: item.Path, Name: item.Name, Width: item.Width, Height: item.Height, Size: item.Size, Status: status, Error: item.Problem, RecordID: item.RecordID, OutputPath: item.OutputPath}
}

// runFromSaved shows a saved batch. Items still queued were interrupted when
// the app quit; they can be retried.
func runFromSaved(batch domain.Batch) *batchRun {
	run := &batchRun{key: batch.ID, meta: batch, parallel: float64(max(1, min(batchMaxParallel, batch.Concurrency)))}
	for _, saved := range batch.Items {
		item := &batchItem{ID: saved.ID, Path: saved.SourcePath, Name: saved.Name, Width: saved.Width, Height: saved.Height, Size: saved.Size, Status: saved.Status, Problem: saved.Error, RecordID: saved.RecordID, OutputPath: saved.OutputPath}
		if item.Status == batchQueued {
			item.Status, item.Problem = batchCanceled, locale.T("已中断")
		}
		run.items = append(run.items, item)
	}
	run.meta.Items = nil
	return run
}

func (a *App) loadBatchRuns() {
	a.run(func() func() {
		batches, err := a.backend.ListBatches()
		return func() {
			if err != nil {
				a.problem = err.Error()
				return
			}
			known := map[string]bool{}
			for _, run := range a.batchRuns {
				known[run.meta.ID] = true
			}
			for _, batch := range batches {
				if !known[batch.ID] {
					a.batchRuns = append(a.batchRuns, runFromSaved(batch))
				}
			}
			a.sortBatchRuns()
		}
	})
}

func (a *App) sortBatchRuns() {
	sort.SliceStable(a.batchRuns, func(i, j int) bool { return a.batchRuns[i].meta.CreatedAt > a.batchRuns[j].meta.CreatedAt })
	a.libraryCache = nil
}

func (a *App) findBatchRun(key string) *batchRun {
	for _, run := range a.batchRuns {
		if run.key == key {
			return run
		}
	}
	return nil
}

// openBatchRun shows a started batch in the batch panel.
func (a *App) openBatchRun(run *batchRun) {
	if run.deleting {
		return
	}
	b := a.batchState()
	b.open, b.view = true, run.key
	a.loadSharedThumbs(run)
}

// startBatch turns the draft's ready images into a new batch, saves it and
// starts sending. The draft keeps its settings and the images not sent.
func (a *App) startBatch() {
	b := a.batchState()
	if !a.canStartBatch() {
		return
	}
	a.saveBatchPreferences()
	request := b.request()
	provider := a.batchProvider()
	run := &batchRun{key: newID(), creating: true, running: true, parallel: b.parallel, shared: request.shared}
	run.meta = domain.Batch{CreatedAt: time.Now().UnixMilli(), Prompt: strings.TrimRight(b.prompt, "\n"), ProviderID: b.providerID, ProviderName: provider.Name, Model: b.model, SizeMode: request.sizeMode, Ratio: b.ratio, Resolution: b.resolution, OutputFormat: b.outputFormat, Concurrency: int(b.parallel), OutputDirectory: b.outputDir, SharedNames: []string{}}
	var shared [][]byte
	for _, ref := range request.shared {
		data, err := referenceBytes(ref)
		if err != nil {
			a.problem = err.Error()
			return
		}
		shared = append(shared, data)
		run.meta.SharedNames = append(run.meta.SharedNames, ref.Name)
		run.sharedThumbs = append(run.sharedThumbs, b.sharedThumbs[ref.ID])
	}
	var kept []*batchItem
	for _, item := range b.items {
		if _, problem, ok := b.currentPlan(item); item.Status != batchReady || (ok && problem != "") {
			kept = append(kept, item)
			continue
		}
		started := &batchItem{ID: item.ID, Path: item.Path, Name: item.Name, Width: item.Width, Height: item.Height, Bytes: item.Bytes, Thumb: item.Thumb, Size: request.itemSize(item), Status: batchQueued}
		run.items = append(run.items, started)
	}
	b.items = kept
	saved := run.meta
	for _, item := range run.items {
		saved.Items = append(saved.Items, item.saved())
	}
	a.batchRuns = append([]*batchRun{run}, a.batchRuns...)
	a.libraryCache = nil
	b.view = run.key
	a.run(func() func() {
		created, err := a.backend.CreateBatch(saved, shared)
		return func() {
			run.creating = false
			if err != nil {
				run.running = false
				for _, item := range run.items {
					item.Status, item.Problem = batchFailed, err.Error()
				}
				a.problem = err.Error()
				a.applyPendingUpdate()
				return
			}
			run.meta.ID, run.meta.CreatedAt, run.meta.SharedFiles = created.ID, created.CreatedAt, created.SharedFiles
			a.sortBatchRuns()
			run.ctx, run.cancel = context.WithCancel(a.ctx)
			a.pumpBatch(run)
		}
	})
}

// retryBatch queues the failed, canceled and interrupted items again.
func (a *App) retryBatch(run *batchRun) {
	if run.running || run.creating || run.deleting || run.meta.ID == "" || a.updateApplying() {
		return
	}
	var queued []*batchItem
	for _, item := range run.items {
		if item.Status == batchFailed || item.Status == batchCanceled {
			item.Status, item.Problem, item.Progress = batchQueued, "", 0
			queued = append(queued, item)
		}
	}
	if len(queued) == 0 {
		return
	}
	a.saveBatchItems(run, queued...)
	run.running, run.paused = true, false
	run.ctx, run.cancel = context.WithCancel(a.ctx)
	if run.shared == nil && len(run.meta.SharedFiles) > 0 {
		a.loadShared(run, func(err error) {
			if err != nil {
				a.failQueued(run, err.Error())
				return
			}
			a.pumpBatch(run)
		})
		return
	}
	a.pumpBatch(run)
}

// loadShared reads a saved batch's shared references for new requests.
func (a *App) loadShared(run *batchRun, then func(error)) {
	id, n := run.meta.ID, len(run.meta.SharedFiles)
	a.run(func() func() {
		var refs []domain.Reference
		var err error
		for i := 0; i < n && err == nil; i++ {
			var data []byte
			if data, err = a.backend.ReadBatchReference(id, i); err == nil {
				mime := "image/png"
				if format, e := generation.ImageFormat(data); e == nil {
					mime = map[string]string{"jpg": "image/jpeg", "webp": "image/webp", "png": "image/png"}[format]
				}
				refs = append(refs, domain.Reference{ID: newID(), Name: run.meta.SharedNames[i], DataURL: generation.DataURL(data, mime)})
			}
		}
		return func() {
			if err == nil {
				run.shared = refs
			}
			then(err)
		}
	})
}

// loadSharedThumbs shows a saved batch's shared references in the panel.
func (a *App) loadSharedThumbs(run *batchRun) {
	if run.sharedLoading || len(run.sharedThumbs) == len(run.meta.SharedFiles) || run.meta.ID == "" {
		return
	}
	run.sharedLoading = true
	id, n := run.meta.ID, len(run.meta.SharedFiles)
	a.run(func() func() {
		thumbs := make([]*ui.Bitmap, n)
		for i := range thumbs {
			if data, err := a.backend.ReadBatchReference(id, i); err == nil {
				thumbs[i], _ = thumbnail(data, referenceThumbSize, referenceThumbSize)
			}
		}
		return func() {
			run.sharedLoading = false
			run.sharedThumbs = thumbs
		}
	})
}

func (a *App) failQueued(run *batchRun, problem string) {
	var changed []*batchItem
	for _, item := range run.items {
		if item.Status == batchQueued {
			item.Status, item.Problem = batchFailed, problem
			changed = append(changed, item)
		}
	}
	a.saveBatchItems(run, changed...)
	a.problem = problem
	run.paused = false
	a.finishBatchIfIdle(run)
}

// stopBatch cancels the requests in flight and every queued item.
func (a *App) stopBatch(run *batchRun) {
	if !run.running {
		return
	}
	run.paused = false
	if run.cancel != nil {
		run.cancel()
	}
	var changed []*batchItem
	for _, item := range run.items {
		if item.Status == batchQueued {
			item.Status, item.Problem = batchCanceled, locale.T("已取消")
			changed = append(changed, item)
		}
	}
	a.saveBatchItems(run, changed...)
	a.finishBatchIfIdle(run)
}

func (a *App) finishBatchIfIdle(run *batchRun) {
	if !run.running || run.creating || run.active > 0 || run.paused {
		return
	}
	for _, item := range run.items {
		if item.Status == batchQueued {
			return
		}
	}
	run.running = false
	run.shared = nil
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
	done, failed, _ := run.counts()
	if failed > 0 {
		a.showNotice(fmt.Sprintf(locale.T("批量处理结束：%d 项完成，%d 项失败。"), done, failed))
	} else {
		a.showNotice(fmt.Sprintf(locale.T("批量处理完成：%d 项。"), done))
	}
	a.refreshHistory()
	a.applyPendingUpdate()
}

// pumpBatch starts queued items while there is room under the limit.
func (a *App) pumpBatch(run *batchRun) {
	if !run.running || run.creating {
		return
	}
	limit := max(1, min(batchMaxParallel, int(run.parallel)))
	for _, item := range run.items {
		if run.paused || run.active >= limit || run.ctx.Err() != nil || a.updateApplying() {
			break
		}
		if item.Status == batchQueued {
			a.runBatchItem(run, item)
		}
	}
	a.finishBatchIfIdle(run)
}

func (a *App) runBatchItem(run *batchRun, item *batchItem) {
	item.Status, item.Progress, item.Problem = batchGenerating, 0, ""
	run.active++
	ctx := run.ctx
	request := run.request()
	outputDir := run.meta.OutputDirectory
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		var record domain.ImageRecord
		var output string
		source, err := a.readBatchSource(item.Path, item.Name)
		if err == nil {
			record, err = a.backend.Generate(ctx, request.itemArgs(item, source), func(p domain.Progress) {
				a.post(func() { item.Progress = p.Progress })
			})
		}
		var saveErr error
		if err == nil && outputDir != "" {
			stem := strings.TrimSuffix(item.Name, filepath.Ext(item.Name))
			output, saveErr = a.desktop.SaveBatchResult(record.ID, outputDir, stem)
		}
		a.post(func() {
			run.active--
			switch {
			case err != nil && ctx.Err() != nil:
				item.Status, item.Problem = batchCanceled, locale.T("已取消")
			case err != nil:
				item.Status, item.Problem = batchFailed, err.Error()
			default:
				item.Status, item.RecordID, item.OutputPath, item.Progress = batchDone, record.ID, output, 100
				if saveErr != nil {
					item.Problem = locale.T("已保存到作品库，但复制到输出目录失败：") + saveErr.Error()
				}
			}
			a.saveBatchItems(run, item)
			a.pumpBatch(run)
		})
	}()
}

// saveBatchItems queues item changes for saving. One write runs at a time,
// so an older state never lands after a newer one.
func (a *App) saveBatchItems(run *batchRun, items ...*batchItem) {
	if run.meta.ID == "" || len(items) == 0 {
		return
	}
	if a.batchSaves == nil {
		a.batchSaves = map[string]map[string]domain.BatchItem{}
	}
	pending := a.batchSaves[run.meta.ID]
	if pending == nil {
		pending = map[string]domain.BatchItem{}
		a.batchSaves[run.meta.ID] = pending
	}
	for _, item := range items {
		pending[item.ID] = item.saved()
	}
	a.flushBatchSaves()
}

func (a *App) flushBatchSaves() {
	if a.batchSaving || len(a.batchSaves) == 0 || a.ctx.Err() != nil {
		return
	}
	saves := a.batchSaves
	a.batchSaves = nil
	a.batchInFlight = saves
	a.batchSaving = true
	a.batchWrites.Add(1)
	a.run(func() func() {
		defer a.batchWrites.Done()
		err := a.writeBatchSaves(saves)
		return func() {
			if a.ctx.Err() != nil {
				return
			}
			a.batchSaving = false
			a.batchInFlight = nil
			if err != nil {
				a.requeueBatchSaves(saves)
				a.problem = err.Error()
				return
			}
			a.flushBatchSaves()
			a.applyPendingUpdate()
		}
	})
}

func (a *App) writeBatchSaves(saves map[string]map[string]domain.BatchItem) error {
	var err error
	for id, items := range saves {
		list := make([]domain.BatchItem, 0, len(items))
		for _, item := range items {
			list = append(list, item)
		}
		if e := a.backend.UpdateBatchItems(id, list); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// Retain failed or in-flight snapshots without replacing newer queued states.
func (a *App) requeueBatchSaves(saves map[string]map[string]domain.BatchItem) {
	if a.batchSaves == nil {
		a.batchSaves = map[string]map[string]domain.BatchItem{}
	}
	for id, items := range saves {
		if a.batchSaves[id] == nil {
			a.batchSaves[id] = map[string]domain.BatchItem{}
		}
		for key, item := range items {
			if _, newer := a.batchSaves[id][key]; !newer {
				a.batchSaves[id][key] = item
			}
		}
	}
}

// Close has waited for the disk worker, so pending callbacks cannot strand
// terminal states or let an older snapshot overwrite them during shutdown.
func (a *App) flushBatchSavesOnClose() {
	a.requeueBatchSaves(a.batchInFlight)
	if err := a.writeBatchSaves(a.batchSaves); err != nil {
		a.problem = err.Error()
	}
	a.batchSaves, a.batchInFlight, a.batchSaving = nil, nil, false
}

// deleteBatch removes a finished batch with all its works.
func (a *App) deleteBatch(key string) {
	run := a.findBatchRun(key)
	if run == nil || run.running || run.creating || run.deleting || a.batchSaving || len(a.batchSaves) > 0 || a.updateApplying() {
		return
	}
	forget := func() {
		for i, r := range a.batchRuns {
			if r == run {
				a.batchRuns = append(a.batchRuns[:i], a.batchRuns[i+1:]...)
				break
			}
		}
		for _, item := range run.items {
			if item.RecordID != "" {
				a.forgetThumb(item.RecordID)
			}
		}
		a.libraryCache = nil
		if b := a.batch; b != nil && b.view == key {
			b.open = false
		}
	}
	if run.meta.ID == "" {
		forget()
		return
	}
	run.deleting = true
	a.batchWrites.Add(1)
	a.run(func() func() {
		defer a.batchWrites.Done()
		err := a.backend.DeleteBatch(run.meta.ID)
		return func() {
			if a.ctx.Err() != nil {
				return
			}
			run.deleting = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			forget()
			a.showNotice(locale.T("批量任务已删除。"))
			a.refreshHistory()
			a.applyPendingUpdate()
		}
	})
}

// openBatchResult opens the work an item made in the usual viewer.
func (a *App) openBatchResult(item *batchItem) {
	id := item.RecordID
	a.run(func() func() {
		page, err := a.backend.ListHistory(domain.HistoryQuery{All: true})
		return func() {
			if err != nil {
				a.problem = err.Error()
				return
			}
			for _, record := range page.Items {
				if record.ID == id {
					a.openArtwork(record)
					return
				}
			}
			a.problem = locale.T("这张作品已被删除。")
		}
	})
}

// BatchRunning reports items still waiting or sending, for the quit prompt.
func (a *App) BatchRunning() int {
	n := 0
	for _, run := range a.batchRuns {
		if !run.running && !run.creating {
			continue
		}
		for _, item := range run.items {
			if item.Status == batchQueued || item.Status == batchGenerating {
				n++
			}
		}
	}
	return n
}
