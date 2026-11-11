package studio

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
	"image2ai/internal/locale"
)

const (
	batchMaxShared       = 3
	batchDefaultParallel = 2
	batchMaxParallel     = 16
	batchThumbSize       = 2 * 48
)

// Batch item states. Items start "pending" until their file is read.
const (
	batchPending    = "pending"
	batchReady      = "ready"
	batchInvalid    = "invalid"
	batchQueued     = "queued"
	batchGenerating = "generating"
	batchDone       = "done"
	batchFailed     = "error"
	batchCanceled   = "canceled"
)

// batchItem is one source image. Only its path, size and a small thumbnail
// stay in memory; its bytes are read again when its request is prepared.
type batchItem struct {
	ID            string
	Path, Name    string
	Width, Height int
	Bytes         int
	Thumb         *ui.Bitmap
	Status        string
	Problem       string
	Progress      int
	Plan          *domain.RequestPlan
	PlanErr       string
	RecordID      string
	OutputPath    string
	expanded      bool
	planning      bool
	planKey       string
}

// batchState is the batch panel's draft and its running queue. Like the rest
// of App it changes only on the UI thread.
type batchState struct {
	open                bool
	items               []*batchItem
	shared              []domain.Reference
	sharedThumbs        map[string]*ui.Bitmap
	prompt              string
	providerID, model   string
	sizeMode            string
	ratio, resolution   string
	outputFormat        string
	parallel            float64
	outputDir           string
	list                ui.ListState
	adding, sharing     bool
	running, paused     bool
	active              int
	cancel              context.CancelFunc
	ctx                 context.Context
	prefsLoaded         bool
	clearOpen, stopOpen bool
}

// batchRequest is the part of a batch's choices one request is built from,
// copied when it is planned or sent so later edits change nothing in flight.
type batchRequest struct {
	prompt, providerID, model   string
	sizeMode, ratio, resolution string
	outputFormat                string
	shared                      []domain.Reference
}

func (b *batchState) request() batchRequest {
	return batchRequest{prompt: b.prompt, providerID: b.providerID, model: b.model, sizeMode: b.sizeMode, ratio: b.ratio, resolution: b.resolution, outputFormat: b.outputFormat, shared: append([]domain.Reference(nil), b.shared...)}
}

func (a *App) batchState() *batchState {
	if a.batch == nil {
		a.batch = &batchState{sharedThumbs: map[string]*ui.Bitmap{}, sizeMode: "source", ratio: "3:4", parallel: batchDefaultParallel}
	}
	b := a.batch
	if !b.prefsLoaded && a.loaded {
		b.prefsLoaded = true
		p := a.config.Batch
		if p.Concurrency > 0 {
			b.parallel = float64(min(batchMaxParallel, p.Concurrency))
		}
		if p.SizeMode != "" {
			b.sizeMode = p.SizeMode
		}
		if p.Ratio != "" {
			b.ratio = p.Ratio
		}
		b.outputDir = p.OutputDirectory
		b.providerID, b.model, b.resolution, b.outputFormat = a.providerID, a.model, a.resolution, a.outputFormat
		if b.outputFormat == "gif" || b.outputFormat == "svg" {
			b.outputFormat = ""
		}
	}
	return b
}

// openBatch shows the batch panel; the composer and its draft are untouched.
func (a *App) openBatch() {
	a.batchState().open = true
	a.reconcileBatch()
	a.loadBatchPricing()
}

func (a *App) loadBatchPricing() {
	id := a.batchState().providerID
	if id == "" || a.priceLoading[id] {
		return
	}
	if _, ok := a.pricing[id]; !ok {
		a.requestPricing(id, false)
	}
}

// batchPrice is the estimated cost of n requests, when the provider says.
func (a *App) batchPrice(n int) string {
	b := a.batchState()
	for _, price := range a.pricing[b.providerID] {
		if price.Model == b.model {
			if value, ok := price.Prices[b.resolution]; ok {
				return fmt.Sprintf(locale.T("预计 %s %g"), price.Currency, math.Round(value*float64(n)*1000)/1000)
			}
		}
	}
	return ""
}

// batchProvider is the batch's provider while it is enabled.
func (a *App) batchProvider() *domain.Provider {
	b := a.batchState()
	for i := range a.config.Providers {
		if a.config.Providers[i].ID == b.providerID && !a.config.Providers[i].Disabled {
			return &a.config.Providers[i]
		}
	}
	return nil
}

func (a *App) reconcileBatch() {
	b := a.batchState()
	p := a.batchProvider()
	if p == nil {
		for i := range a.config.Providers {
			if !a.config.Providers[i].Disabled {
				p = &a.config.Providers[i]
				b.providerID = p.ID
				break
			}
		}
	}
	if p == nil {
		b.model = ""
		return
	}
	if !contains(p.Models, b.model) {
		b.model = first(p.Models)
	}
	if !contains(p.Resolutions, b.resolution) {
		b.resolution = providerDefaultResolution(p)
	}
	if !contains(ratios, b.ratio) {
		b.ratio = "3:4"
	}
}

// nearestRatio is the supported ratio closest to w×h on a log scale, so
// 4000×3000 is 4:3 and a slightly wider panorama is 16:9.
func nearestRatio(w, h int) string {
	if w <= 0 || h <= 0 {
		return "1:1"
	}
	target := math.Log(float64(w) / float64(h))
	best, distance := "1:1", math.Inf(1)
	for _, ratio := range ratios {
		var rw, rh float64
		fmt.Sscanf(ratio, "%f:%f", &rw, &rh)
		if d := math.Abs(math.Log(rw/rh) - target); d < distance {
			best, distance = ratio, d
		}
	}
	return best
}

// itemSize is the size sent for item under the batch's size choice.
func (b batchRequest) itemSize(item *batchItem) string {
	ratio := b.ratio
	if b.sizeMode == "source" {
		ratio = nearestRatio(item.Width, item.Height)
	}
	return computeSize(ratio, b.resolution)
}

// itemArgs is what Generate receives for item: the source image first, then
// the shared references. Source bytes come from ref, read just before.
func (b batchRequest) itemArgs(item *batchItem, source domain.Reference) domain.GenerateArgs {
	args := domain.GenerateArgs{JobID: newID(), Prompt: strings.TrimRight(b.prompt, "\n"), ProviderID: b.providerID, Model: b.model, Size: b.itemSize(item), OutputFormat: b.outputFormat, ReferenceImages: []string{source.DataURL}}
	for _, ref := range b.shared {
		args.ReferenceImages = append(args.ReferenceImages, ref.DataURL)
	}
	return args
}

func (b *batchState) counts() (ready, invalid, done, failed, waiting int) {
	for _, item := range b.items {
		switch item.Status {
		case batchReady:
			if _, problem, ok := b.currentPlan(item); ok && problem != "" {
				invalid++
			} else {
				ready++
			}
		case batchInvalid:
			invalid++
		case batchDone:
			done++
		case batchFailed, batchCanceled:
			failed++
		case batchQueued, batchGenerating, batchPending:
			waiting++
		}
	}
	return
}

func (a *App) batchOpen() bool { return a.batch != nil && a.batch.open }

// batchBusy reports a batch still reading files or sending requests, which
// closing the app or restarting for an update would lose.
func (a *App) batchBusy() bool {
	if a.batch == nil {
		return false
	}
	b := a.batch
	return b.running || b.adding || b.sharing
}

func (a *App) batchProblem() string {
	b := a.batchState()
	switch {
	case strings.TrimSpace(b.prompt) == "":
		return locale.T("请输入提示词。")
	case utf8.RuneCountInString(b.prompt) > 20000:
		return locale.T("提示词不能超过 20000 字。")
	case a.batchProvider() == nil || b.model == "":
		return locale.T("请选择已配置的提供商和模型。")
	case len(b.items) == 0:
		return locale.T("请添加待处理图。")
	}
	return ""
}

func (a *App) canStartBatch() bool {
	b := a.batchState()
	ready, _, _, _, _ := b.counts()
	return !b.running && !b.adding && !b.sharing && a.batchProblem() == "" && ready > 0 && !a.updateApplying()
}

// addBatchFiles reads the size and a thumbnail of each new path in the
// background; the image bytes are not kept.
func (a *App) addBatchFiles(collect func() ([]string, error)) {
	b := a.batchState()
	if b.adding || b.running {
		return
	}
	b.adding = true
	known := map[string]bool{}
	for _, item := range b.items {
		known[item.Path] = true
	}
	a.run(func() func() {
		paths, err := collect()
		var added []*batchItem
		for _, path := range paths {
			if known[path] {
				continue
			}
			known[path] = true
			item := &batchItem{ID: newID(), Path: path, Name: filepath.Base(path), Status: batchReady}
			ref, e := a.desktop.ImportFiles([]string{path}, 1)
			if e == nil && len(ref) == 1 {
				if data, de := referenceBytes(ref[0]); de == nil {
					item.Bytes = len(data)
					if size, se := generation.ImageSize(data); se == nil {
						item.Width, item.Height = size.X, size.Y
					}
					item.Thumb, _ = thumbnail(data, batchThumbSize, batchThumbSize)
				}
			} else {
				item.Status = batchInvalid
				item.Problem = locale.T("图片无法读取或无效。")
				if e != nil {
					item.Problem = e.Error()
				}
			}
			added = append(added, item)
		}
		return func() {
			b.adding = false
			if err != nil {
				a.problem = err.Error()
			}
			if len(paths) > 0 && len(added) == 0 && err == nil {
				a.showNotice(locale.T("所选图片都已在列表中。"))
			}
			b.items = append(b.items, added...)
			a.applyPendingUpdate()
		}
	})
}

func (a *App) addBatchShared(read func(int) ([]domain.Reference, error)) {
	b := a.batchState()
	if b.sharing || b.running || len(b.shared) >= batchMaxShared {
		return
	}
	remaining := batchMaxShared - len(b.shared)
	b.sharing = true
	a.run(func() func() {
		refs, err := read(remaining)
		if len(refs) > remaining {
			refs = refs[:remaining]
		}
		thumbs := map[string]*ui.Bitmap{}
		for i := range refs {
			if refs[i].ID == "" {
				refs[i].ID = newID()
			}
			if data, e := referenceBytes(refs[i]); e == nil {
				thumbs[refs[i].ID], _ = thumbnail(data, referenceThumbSize, referenceThumbSize)
			}
		}
		return func() {
			b.sharing = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			for _, ref := range refs {
				if len(b.shared) < batchMaxShared {
					b.shared = append(b.shared, ref)
					b.sharedThumbs[ref.ID] = thumbs[ref.ID]
				}
			}
			a.applyPendingUpdate()
		}
	})
}

func (a *App) removeBatchShared(id string) {
	b := a.batchState()
	for i, ref := range b.shared {
		if ref.ID == id {
			b.shared = append(b.shared[:i], b.shared[i+1:]...)
			delete(b.sharedThumbs, id)
			return
		}
	}
}

func (a *App) removeBatchItem(id string) {
	b := a.batchState()
	for i, item := range b.items {
		if item.ID == id && item.Status != batchQueued && item.Status != batchGenerating {
			b.items = append(b.items[:i], b.items[i+1:]...)
			return
		}
	}
}

// planKey names everything item's request is built from, so a plan made
// for other choices is never shown as the current one.
func (b *batchState) planKey(item *batchItem) string {
	r := b.request()
	ids := make([]string, len(r.shared))
	for i, ref := range r.shared {
		ids[i] = ref.ID
	}
	return fmt.Sprintf("%q", []string{item.ID, r.prompt, r.providerID, r.model, r.itemSize(item), r.outputFormat, strings.Join(ids, ",")})
}

// currentPlan is item's plan, or its problem, when made for the current
// choices.
func (b *batchState) currentPlan(item *batchItem) (*domain.RequestPlan, string, bool) {
	if item.planKey == "" || item.planKey != b.planKey(item) {
		return nil, "", false
	}
	return item.Plan, item.PlanErr, true
}

// planItem asks the backend for item's request: the validation and
// assembly Generate uses, without sending anything.
func (a *App) planItem(item *batchItem) {
	b := a.batchState()
	key := b.planKey(item)
	if item.Status != batchReady || item.planning || item.planKey == key || a.batchProblem() != "" {
		return
	}
	item.planning, item.planKey = true, key
	request := b.request()
	a.run(func() func() {
		var plan *domain.RequestPlan
		problem := ""
		source, err := a.readBatchSource(item)
		if err == nil {
			var p domain.RequestPlan
			p, err = a.backend.PlanGeneration(request.itemArgs(item, source))
			plan = &p
		}
		if err != nil {
			plan, problem = nil, err.Error()
		}
		return func() {
			item.planning = false
			if item.planKey == key {
				item.Plan, item.PlanErr = plan, problem
			}
		}
	})
}

// readBatchSource reads an item's file now, validated as any reference.
func (a *App) readBatchSource(item *batchItem) (domain.Reference, error) {
	refs, err := a.desktop.ImportFiles([]string{item.Path}, 1)
	if err != nil {
		return domain.Reference{}, err
	}
	if len(refs) != 1 {
		return domain.Reference{}, fmt.Errorf(locale.T("无法读取：%s"), item.Name)
	}
	return refs[0], nil
}

// startBatch queues every ready item and starts up to the chosen number of
// requests. Settings are captured now; editing them later changes nothing
// already queued.
func (a *App) startBatch() {
	b := a.batchState()
	if !a.canStartBatch() {
		return
	}
	a.saveBatchPreferences()
	for _, item := range b.items {
		if _, problem, ok := b.currentPlan(item); item.Status == batchReady && (!ok || problem == "") {
			item.Status, item.Problem, item.Progress = batchQueued, "", 0
		}
	}
	b.ctx, b.cancel = context.WithCancel(a.ctx)
	b.running, b.paused = true, false
	a.pumpBatch()
}

// retryBatchFailures queues the failed and canceled items again.
func (a *App) retryBatchFailures() {
	b := a.batchState()
	if b.running || a.updateApplying() {
		return
	}
	any := false
	for _, item := range b.items {
		if item.Status == batchFailed || item.Status == batchCanceled {
			item.Status, item.Problem, item.Progress = batchQueued, "", 0
			any = true
		}
	}
	if !any {
		return
	}
	b.ctx, b.cancel = context.WithCancel(a.ctx)
	b.running, b.paused = true, false
	a.pumpBatch()
}

// stopBatch cancels the requests in flight and every queued item.
func (a *App) stopBatch() {
	b := a.batchState()
	if !b.running {
		return
	}
	b.paused = false
	if b.cancel != nil {
		b.cancel()
	}
	for _, item := range b.items {
		if item.Status == batchQueued {
			item.Status, item.Problem = batchCanceled, locale.T("已取消")
		}
	}
	a.finishBatchIfIdle()
}

func (a *App) finishBatchIfIdle() {
	b := a.batchState()
	if !b.running || b.active > 0 {
		return
	}
	for _, item := range b.items {
		if item.Status == batchQueued && !b.paused {
			return
		}
	}
	if b.paused {
		return
	}
	b.running = false
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	_, _, done, failed, _ := b.counts()
	if failed > 0 {
		a.showNotice(fmt.Sprintf(locale.T("批量处理结束：%d 项完成，%d 项失败。"), done, failed))
	} else {
		a.showNotice(fmt.Sprintf(locale.T("批量处理完成：%d 项。"), done))
	}
	a.refreshHistory()
	a.applyPendingUpdate()
}

// pumpBatch starts queued items while there is room under the limit.
func (a *App) pumpBatch() {
	b := a.batchState()
	if !b.running {
		return
	}
	limit := max(1, min(batchMaxParallel, int(b.parallel)))
	for _, item := range b.items {
		if b.paused || b.active >= limit || b.ctx.Err() != nil || a.updateApplying() {
			break
		}
		if item.Status != batchQueued {
			continue
		}
		a.runBatchItem(item)
	}
	a.finishBatchIfIdle()
}

func (a *App) runBatchItem(item *batchItem) {
	b := a.batchState()
	item.Status, item.Progress, item.Problem = batchGenerating, 0, ""
	b.active++
	ctx := b.ctx
	snapshot := b.request()
	outputDir := b.outputDir
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		var record domain.ImageRecord
		var output string
		source, err := a.readBatchSource(item)
		if err == nil {
			args := snapshot.itemArgs(item, source)
			record, err = a.backend.Generate(ctx, args, func(p domain.Progress) {
				a.post(func() { item.Progress = p.Progress })
			})
		}
		var saveErr error
		if err == nil && outputDir != "" {
			stem := strings.TrimSuffix(item.Name, filepath.Ext(item.Name))
			output, saveErr = a.desktop.SaveBatchResult(record.ID, outputDir, stem)
		}
		a.post(func() {
			b.active--
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
			a.pumpBatch()
		})
	}()
}

func (a *App) saveBatchPreferences() {
	b := a.batchState()
	prefs := domain.BatchPreferences{Concurrency: int(b.parallel), SizeMode: b.sizeMode, Ratio: b.ratio, OutputDirectory: b.outputDir}
	a.run(func() func() {
		config, err := a.backend.SaveBatchPreferences(prefs)
		return func() {
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.config.Batch = config.Batch
		}
	})
}

func (a *App) chooseBatchOutput() {
	b := a.batchState()
	current := b.outputDir
	a.run(func() func() {
		dir, ok, err := a.desktop.ChooseDirectory(locale.T("选择批量输出目录"), current)
		return func() {
			if err != nil {
				a.problem = err.Error()
				return
			}
			if ok {
				b.outputDir = dir
				a.saveBatchPreferences()
			}
		}
	})
}

// clearBatch empties the list once nothing is running.
func (a *App) clearBatch() {
	b := a.batchState()
	if b.running || b.adding {
		return
	}
	b.items = nil
}

// BatchRunning reports items still waiting or sending, for the quit prompt.
func (a *App) BatchRunning() int {
	if a.batch == nil || !a.batch.running {
		return 0
	}
	n := 0
	for _, item := range a.batch.items {
		if item.Status == batchQueued || item.Status == batchGenerating {
			n++
		}
	}
	return n
}
