package studio

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

// batchBackend keeps batches in memory, records each request and holds it
// until released, so a test can see how many run at once.
type batchBackend struct {
	*testBackend
	mu       sync.Mutex
	inFlight int
	peak     int
	sent     []domain.GenerateArgs
	release  chan struct{}
	failName string
	prefs    domain.BatchPreferences
	batches  []domain.Batch
	shared   map[string][][]byte
}

func (b *batchBackend) PlanGeneration(args domain.GenerateArgs) (domain.RequestPlan, error) {
	return domain.RequestPlan{Method: "POST", URL: "http://127.0.0.1/images/edits", ContentType: "multipart/form-data", Fields: []domain.RequestField{{Name: "size", Value: args.Size}}}, nil
}
func (b *batchBackend) SaveBatchPreferences(p domain.BatchPreferences) (domain.Config, error) {
	b.mu.Lock()
	b.prefs = p
	b.mu.Unlock()
	b.testBackend.mu.Lock()
	defer b.testBackend.mu.Unlock()
	b.config.Batch = p
	return b.config, nil
}
func (b *batchBackend) ListBatches() ([]domain.Batch, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.batches), nil
}
func (b *batchBackend) CreateBatch(batch domain.Batch, shared [][]byte) (domain.Batch, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	batch.ID = newID()[:16]
	batch.SharedFiles = make([]string, len(shared))
	if b.shared == nil {
		b.shared = map[string][][]byte{}
	}
	b.shared[batch.ID] = shared
	batch.Items = slices.Clone(batch.Items)
	b.batches = append(b.batches, batch)
	return batch, nil
}
func (b *batchBackend) UpdateBatchItems(id string, items []domain.BatchItem) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.batches {
		if b.batches[i].ID == id {
			for _, item := range items {
				for j := range b.batches[i].Items {
					if b.batches[i].Items[j].ID == item.ID {
						b.batches[i].Items[j] = item
					}
				}
			}
			return nil
		}
	}
	return errors.New("批次不存在")
}
func (b *batchBackend) ReadBatchReference(id string, index int) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if refs := b.shared[id]; index < len(refs) {
		return refs[index], nil
	}
	return nil, errors.New("参考图不存在")
}
func (b *batchBackend) DeleteBatch(id string) error {
	b.mu.Lock()
	for i, batch := range b.batches {
		if batch.ID == id {
			b.batches = append(b.batches[:i], b.batches[i+1:]...)
			break
		}
	}
	b.mu.Unlock()
	b.testBackend.mu.Lock()
	defer b.testBackend.mu.Unlock()
	b.records = slices.DeleteFunc(b.records, func(r domain.ImageRecord) bool { return r.BatchID == id })
	return nil
}
func (b *batchBackend) savedItems(id string) []domain.BatchItem {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, batch := range b.batches {
		if batch.ID == id {
			return slices.Clone(batch.Items)
		}
	}
	return nil
}
func (b *batchBackend) Generate(ctx context.Context, args domain.GenerateArgs, progress func(domain.Progress)) (domain.ImageRecord, error) {
	b.mu.Lock()
	b.inFlight++
	b.peak = max(b.peak, b.inFlight)
	b.sent = append(b.sent, args)
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.inFlight--; b.mu.Unlock() }()
	select {
	case <-b.release:
	case <-ctx.Done():
		return domain.ImageRecord{}, ctx.Err()
	}
	if b.failName != "" && args.ReferenceImages[0] == b.failName {
		return domain.ImageRecord{}, errors.New("模拟失败")
	}
	record := domain.ImageRecord{ID: args.JobID, Prompt: args.Prompt, Model: args.Model, Size: args.Size, BatchID: args.BatchID, CreatedAt: time.Now().UnixMilli(), Width: 48, Height: 32}
	b.testBackend.mu.Lock()
	b.records = append(b.records, record)
	b.testBackend.mu.Unlock()
	return record, nil
}

// batchDesktop serves images from memory, keyed by path, and records the
// copies made to the output directory.
type batchDesktop struct {
	*testDesktop
	files  map[string][]byte
	mu     sync.Mutex
	copies []string
}

func (d *batchDesktop) ImportFiles(paths []string, _ int) ([]domain.Reference, error) {
	data, ok := d.files[paths[0]]
	if !ok {
		return nil, errors.New("无法读取")
	}
	return []domain.Reference{{ID: newID(), Name: filepath.Base(paths[0]), DataURL: pngURL(data)}}, nil
}
func (d *batchDesktop) CollectImages(paths []string) ([]string, error) { return paths, nil }
func (d *batchDesktop) OpenBatchImages() ([]string, error)             { return nil, nil }
func (d *batchDesktop) SaveBatchResult(id, dir, stem string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.copies = append(d.copies, stem)
	return filepath.Join(dir, stem+".png"), nil
}

func pngURL(data []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

func sizedPNG(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func batchFixture(t *testing.T, names ...string) (*App, *ui.Tester, *batchBackend, *batchDesktop, []string) {
	t.Helper()
	a, tt, base, d := fixture(t, 1280, 900)
	backend := &batchBackend{testBackend: base, release: make(chan struct{})}
	desktop := &batchDesktop{testDesktop: d, files: map[string][]byte{}}
	a.backend, a.desktop = backend, desktop
	var paths []string
	for i, name := range names {
		path := filepath.Join("in", name)
		paths = append(paths, path)
		desktop.files[path] = sizedPNG(40+i*20, 30)
	}
	a.openBatch()
	a.addBatchFiles(func() ([]string, error) { return paths, nil })
	settle(a, tt)
	return a, tt, backend, desktop, paths
}

// releaseOne lets one held request finish, draining UI work meanwhile so
// the batch can start the next one.
func releaseOne(t *testing.T, a *App, backend *batchBackend) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case backend.release <- struct{}{}:
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("no request waiting")
		}
		a.drain()
		time.Sleep(2 * time.Millisecond)
	}
}

// waitFor drains UI work until done reports true.
func waitFor(t *testing.T, a *App, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		a.drain()
		time.Sleep(2 * time.Millisecond)
	}
}

func TestNearestRatioFollowsSourceAspect(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		want string
	}{{4000, 3000, "4:3"}, {3000, 4000, "3:4"}, {1000, 1000, "1:1"}, {1920, 1080, "16:9"}, {1080, 1920, "9:16"}, {2560, 1080, "21:9"}, {0, 10, "1:1"}} {
		if got := nearestRatio(tc.w, tc.h); got != tc.want {
			t.Errorf("nearestRatio(%d, %d) = %s, want %s", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestBatchRunsWithinLimitSourceFirstAndShowsAsOneCard(t *testing.T) {
	a, tt, backend, desktop, paths := batchFixture(t, "a.png", "b.png", "c.png", "d.png", "e.png")
	b := a.batch
	if len(b.items) != 5 || b.items[0].Width != 40 || b.items[0].Thumb == nil {
		t.Fatalf("items not read: %d", len(b.items))
	}
	a.addBatchFiles(func() ([]string, error) { return paths[:2], nil })
	settle(a, tt)
	if len(b.items) != 5 {
		t.Fatal("repeated images were added twice")
	}
	draft := a.prompt
	backend.failName = pngURL(desktop.files[paths[2]])
	b.prompt = "把背景换成纯白"
	b.shared = []domain.Reference{{ID: "shared", Name: "style.png", DataURL: pngURL(sizedPNG(8, 8))}}
	b.parallel = 2
	b.outputDir = filepath.Join(t.TempDir(), "out")
	settle(a, tt)
	if !tt.HasText("就绪") {
		t.Fatal("rows on screen were not checked before sending")
	}
	click(t, tt, "开始批量")
	if len(a.batchRuns) != 1 || len(b.items) != 0 || b.view != a.batchRuns[0].key {
		t.Fatal("starting did not turn the draft into a batch shown in the panel")
	}
	run := a.batchRuns[0]
	waitFor(t, a, func() bool { backend.mu.Lock(); defer backend.mu.Unlock(); return len(backend.sent) == 2 })
	if a.prompt != draft || len(a.jobs) != 0 {
		t.Fatal("batch changed the composer draft or its jobs")
	}
	for i := 0; i < 5; i++ {
		releaseOne(t, a, backend)
	}
	waitFor(t, a, func() bool { return !run.running })
	settle(a, tt)
	backend.mu.Lock()
	peak, sent := backend.peak, slices.Clone(backend.sent)
	backend.mu.Unlock()
	if peak != 2 || len(sent) != 5 {
		t.Fatalf("ran %d at once (limit 2), sent %d", peak, len(sent))
	}
	// Two run at once, so requests may reach the backend in either order.
	bySource := map[string]domain.GenerateArgs{}
	for _, args := range sent {
		if len(args.ReferenceImages) != 2 || args.ReferenceImages[1] != b.shared[0].DataURL || args.Prompt != "把背景换成纯白" || args.BatchID != run.meta.ID {
			t.Fatal("a request does not send its source first, then the shared reference, the prompt and the batch")
		}
		bySource[args.ReferenceImages[0]] = args
	}
	for i, path := range paths {
		args, ok := bySource[pngURL(desktop.files[path])]
		if !ok {
			t.Fatalf("source %d was never sent first", i)
		}
		if want := computeSize(nearestRatio(40+i*20, 30), b.resolution); args.Size != want {
			t.Fatalf("source %d sent as %s, want %s", i, args.Size, want)
		}
	}
	done, failed, _ := run.counts()
	if done != 4 || failed != 1 || run.items[2].Status != batchFailed {
		t.Fatalf("done %d failed %d", done, failed)
	}
	copies := slices.Clone(desktop.copies)
	slices.Sort(copies)
	if strings.Join(copies, ",") != "a,b,d,e" {
		t.Fatalf("output copies %v", desktop.copies)
	}
	saved := backend.savedItems(run.meta.ID)
	if len(saved) != 5 || saved[0].Status != batchDone || saved[0].RecordID == "" || saved[2].Status != batchFailed || saved[2].Error == "" {
		t.Fatalf("item results not saved: %+v", saved)
	}
	entries := a.libraryEntries()
	if len(entries) != 1 || entries[0].run != run || len(a.history.Items) != 4 {
		t.Fatalf("library shows %d cards for one batch of %d works", len(entries), len(a.history.Items))
	}
	b.open = false
	settle(a, tt)
	if !tt.HasText("批量 · 5 张") {
		t.Fatal("batch card is not marked as a batch")
	}
	click(t, tt, "打开批量任务 把背景换成纯白")
	if !a.batchOpen() || b.view != run.key || !tt.HasText("批量任务") {
		t.Fatal("clicking the card did not open its batch")
	}
	backend.failName = ""
	click(t, tt, "重试失败项 1")
	releaseOne(t, a, backend)
	waitFor(t, a, func() bool { return !run.running })
	if run.items[2].Status != batchDone || backend.savedItems(run.meta.ID)[2].Status != batchDone {
		t.Fatalf("retry left %s", run.items[2].Status)
	}
	if backend.prefs.Concurrency != 2 || backend.prefs.OutputDirectory != b.outputDir {
		t.Fatalf("batch choices not remembered: %+v", backend.prefs)
	}
}

func TestBatchCancelStopsQueueAndBlocksUpdates(t *testing.T) {
	a, _, backend, _, _ := batchFixture(t, "a.png", "b.png", "c.png")
	a.batch.prompt = "统一调色"
	a.batch.parallel = 1
	a.startBatch()
	run := a.batchRuns[0]
	if !a.updateBusy() || a.BatchRunning() != 3 {
		t.Fatalf("running batch does not hold updates: %d", a.BatchRunning())
	}
	waitFor(t, a, func() bool { return !run.creating })
	waitFor(t, a, func() bool { backend.mu.Lock(); defer backend.mu.Unlock(); return len(backend.sent) == 1 })
	a.stopBatch(run)
	waitFor(t, a, func() bool { return !run.running })
	backend.mu.Lock()
	sent := len(backend.sent)
	backend.mu.Unlock()
	if sent != 1 {
		t.Fatalf("cancel still sent %d requests", sent)
	}
	for _, item := range run.items {
		if item.Status != batchCanceled {
			t.Fatalf("item %s is %s after cancel", item.Name, item.Status)
		}
	}
	if a.BatchRunning() != 0 || a.batchBusy() {
		t.Fatal("canceled batch still reported running")
	}
}

func TestSavedBatchReopensAsOneCardAndDeletesItsWorks(t *testing.T) {
	a, tt, base, d := fixture(t, 1280, 900)
	backend := &batchBackend{testBackend: base, release: make(chan struct{})}
	a.backend, a.desktop = backend, &batchDesktop{testDesktop: d, files: map[string][]byte{}}
	backend.batches = []domain.Batch{{ID: "saved", CreatedAt: 100, Prompt: "上次的批量", Model: "studio-image", Items: []domain.BatchItem{
		{ID: "1", Name: "one.png", Status: batchDone, RecordID: "r1"},
		{ID: "2", Name: "two.png", Status: batchQueued},
	}}}
	base.records = []domain.ImageRecord{{ID: "r1", Prompt: "上次的批量", BatchID: "saved", CreatedAt: 100}, {ID: "single", Prompt: "单张", CreatedAt: 200}}
	a.loadBatchRuns()
	a.refreshHistory()
	settle(a, tt)
	entries := a.libraryEntries()
	if len(entries) != 2 || entries[0].record == nil || entries[0].record.ID != "single" || entries[1].run == nil {
		t.Fatalf("library should show the single work and one batch card, got %d", len(entries))
	}
	run := entries[1].run
	if run.items[1].Status != batchCanceled || run.items[1].Problem != "已中断" {
		t.Fatal("an item left queued by a quit is not shown as interrupted")
	}
	// The store filters by search; here only the single work matches.
	a.search = "单张"
	a.setHistory(domain.HistoryPage{Items: []domain.ImageRecord{base.records[1]}, Total: 1})
	if entries := a.libraryEntries(); len(entries) != 1 || entries[0].record == nil {
		t.Fatal("search showed a batch with no matching work")
	}
	a.setHistory(domain.HistoryPage{Items: []domain.ImageRecord{base.records[0]}, Total: 1})
	if entries := a.libraryEntries(); len(entries) != 1 || entries[0].run != run {
		t.Fatal("search hid a batch whose work matches")
	}
	a.search = ""
	a.refreshHistory()
	settle(a, tt)
	a.confirmDeleteBatch(run)
	tt.Frame()
	click(t, tt, "删除批量任务")
	settle(a, tt)
	if len(a.batchRuns) != 0 || len(backend.batches) != 0 || len(a.history.Items) != 1 || a.history.Items[0].ID != "single" {
		t.Fatal("deleting the batch did not remove it and its works only")
	}
}
