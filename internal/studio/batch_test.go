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

	"image2ai/internal/domain"
)

// batchBackend records each request and holds it until released, so a
// test can see how many run at once.
type batchBackend struct {
	*testBackend
	mu       sync.Mutex
	inFlight int
	peak     int
	sent     []domain.GenerateArgs
	planned  []domain.GenerateArgs
	release  chan struct{}
	failName string
	prefs    domain.BatchPreferences
}

func (b *batchBackend) PlanGeneration(args domain.GenerateArgs) (domain.RequestPlan, error) {
	b.mu.Lock()
	b.planned = append(b.planned, args)
	b.mu.Unlock()
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
	return domain.ImageRecord{ID: args.JobID, Prompt: args.Prompt, Size: args.Size}, nil
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

func TestBatchRunsWithinLimitSourceFirstAndCopiesOutput(t *testing.T) {
	a, tt, base, d := fixture(t, 1280, 900)
	backend := &batchBackend{testBackend: base, release: make(chan struct{})}
	desktop := &batchDesktop{testDesktop: d, files: map[string][]byte{}}
	a.backend, a.desktop = backend, desktop
	var paths []string
	for i, name := range []string{"a.png", "b.png", "c.png", "d.png", "e.png"} {
		path := filepath.Join("in", name)
		paths = append(paths, path)
		desktop.files[path] = sizedPNG(40+i*20, 30)
	}
	backend.failName = pngURL(desktop.files[paths[2]])
	draft := a.prompt
	click(t, tt, "批量处理")
	if !a.batchOpen() {
		t.Fatal("batch panel did not open")
	}
	b := a.batch
	a.addBatchFiles(func() ([]string, error) { return paths, nil })
	settle(a, tt)
	if len(b.items) != 5 || b.items[0].Width != 40 || b.items[0].Thumb == nil {
		t.Fatalf("items not read: %d", len(b.items))
	}
	a.addBatchFiles(func() ([]string, error) { return paths[:2], nil })
	settle(a, tt)
	if len(b.items) != 5 {
		t.Fatal("repeated images were added twice")
	}
	b.prompt = "把背景换成纯白"
	b.shared = []domain.Reference{{ID: "shared", Name: "style.png", DataURL: pngURL(sizedPNG(8, 8))}}
	b.parallel = 2
	b.outputDir = filepath.Join(t.TempDir(), "out")
	settle(a, tt)
	if !a.canStartBatch() {
		t.Fatalf("batch cannot start: %s", a.batchProblem())
	}
	if !tt.HasText("就绪") {
		t.Fatal("rows on screen were not checked before sending")
	}
	click(t, tt, "开始批量")
	deadline := time.Now().Add(5 * time.Second)
	for {
		backend.mu.Lock()
		sent := len(backend.sent)
		backend.mu.Unlock()
		if sent == 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if a.prompt != draft || len(a.jobs) != 0 {
		t.Fatal("batch changed the composer draft or its jobs")
	}
	for i := 0; i < 5; i++ {
		backend.release <- struct{}{}
		time.Sleep(10 * time.Millisecond)
		a.drain()
	}
	for b.running && time.Now().Before(deadline) {
		a.drain()
		time.Sleep(5 * time.Millisecond)
	}
	settle(a, tt)
	backend.mu.Lock()
	peak, sent := backend.peak, append([]domain.GenerateArgs(nil), backend.sent...)
	backend.mu.Unlock()
	if peak != 2 {
		t.Fatalf("ran %d at once, limit 2", peak)
	}
	if len(sent) != 5 {
		t.Fatalf("sent %d requests", len(sent))
	}
	// Two run at once, so requests may reach the backend in either order.
	bySource := map[string]domain.GenerateArgs{}
	for _, args := range sent {
		if len(args.ReferenceImages) != 2 || args.ReferenceImages[1] != b.shared[0].DataURL || args.Prompt != "把背景换成纯白" {
			t.Fatal("a request does not send its source first, then the shared reference and the prompt")
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
	_, _, done, failed, _ := b.counts()
	if done != 4 || failed != 1 || b.items[2].Status != batchFailed {
		t.Fatalf("done %d failed %d", done, failed)
	}
	copies := append([]string(nil), desktop.copies...)
	slices.Sort(copies)
	if strings.Join(copies, ",") != "a,b,d,e" {
		t.Fatalf("output copies %v", desktop.copies)
	}
	if backend.prefs.Concurrency != 2 || backend.prefs.OutputDirectory != b.outputDir {
		t.Fatalf("batch choices not remembered: %+v", backend.prefs)
	}
	backend.failName = ""
	click(t, tt, "重试失败项 1")
	backend.release <- struct{}{}
	for b.running && time.Now().Before(deadline) {
		a.drain()
		time.Sleep(5 * time.Millisecond)
	}
	if b.items[2].Status != batchDone {
		t.Fatalf("retry left %s", b.items[2].Status)
	}
}

func TestBatchCancelStopsQueueAndBlocksUpdates(t *testing.T) {
	a, tt, base, d := fixture(t, 1280, 900)
	backend := &batchBackend{testBackend: base, release: make(chan struct{})}
	desktop := &batchDesktop{testDesktop: d, files: map[string][]byte{}}
	a.backend, a.desktop = backend, desktop
	var paths []string
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		paths = append(paths, name)
		desktop.files[name] = sizedPNG(32, 32)
	}
	a.openBatch()
	a.addBatchFiles(func() ([]string, error) { return paths, nil })
	settle(a, tt)
	a.batch.prompt = "统一调色"
	a.batch.parallel = 1
	a.startBatch()
	if !a.updateBusy() || a.BatchRunning() != 3 {
		t.Fatalf("running batch does not hold updates: %d", a.BatchRunning())
	}
	a.stopBatch()
	deadline := time.Now().Add(5 * time.Second)
	for a.batch.running && time.Now().Before(deadline) {
		a.drain()
		time.Sleep(5 * time.Millisecond)
	}
	backend.mu.Lock()
	sent := len(backend.sent)
	backend.mu.Unlock()
	if sent != 1 {
		t.Fatalf("cancel still sent %d requests", sent)
	}
	for _, item := range a.batch.items {
		if item.Status != batchCanceled {
			t.Fatalf("item %s is %s after cancel", item.Name, item.Status)
		}
	}
	if a.BatchRunning() != 0 || a.batchBusy() {
		t.Fatal("canceled batch still reported running")
	}
}
