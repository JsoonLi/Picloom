package studio

import (
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"image2ai/internal/domain"
)

type lifecycleBatchBackend struct {
	*batchBackend
	saveStarted   chan []domain.BatchItem
	saveRelease   chan struct{}
	deleteStarted chan struct{}
	deleteRelease chan struct{}
	deleteCalls   atomic.Int32
}

func (b *lifecycleBatchBackend) UpdateBatchItems(id string, items []domain.BatchItem) error {
	if b.saveStarted != nil {
		b.saveStarted <- slices.Clone(items)
		<-b.saveRelease
	}
	return b.batchBackend.UpdateBatchItems(id, items)
}

func (b *lifecycleBatchBackend) DeleteBatch(id string) error {
	b.deleteCalls.Add(1)
	if b.deleteStarted != nil {
		b.deleteStarted <- struct{}{}
		<-b.deleteRelease
	}
	return b.batchBackend.DeleteBatch(id)
}

func savedLifecycleBatch() domain.Batch {
	return domain.Batch{ID: "saved", Prompt: "统一调色", ProviderID: "local", Model: "studio-image", SizeMode: "fixed", Ratio: "3:4", Resolution: "2k", Concurrency: 1, Items: []domain.BatchItem{
		{ID: "first", Name: "first.png", Status: batchQueued},
		{ID: "second", Name: "second.png", Status: batchQueued},
	}}
}

func TestBatchUpdateWaitsForEveryStatusWrite(t *testing.T) {
	a, tt, base, desktop := updateFixture(t)
	batch := savedLifecycleBatch()
	backend := &lifecycleBatchBackend{batchBackend: &batchBackend{testBackend: base, batches: []domain.Batch{batch}}, saveStarted: make(chan []domain.BatchItem, 8), saveRelease: make(chan struct{})}
	t.Cleanup(func() { close(backend.saveRelease) })
	a.backend = backend
	run := runFromSaved(batch)
	a.batchRuns = []*batchRun{run}
	run.running = true
	run.items[0].Status, run.items[0].RecordID = batchDone, "result-first"
	a.saveBatchItems(run, run.items[0])
	select {
	case <-backend.saveStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first batch write did not start")
	}
	run.items[1].Status, run.items[1].RecordID = batchDone, "result-second"
	a.saveBatchItems(run, run.items[1])
	desktop.updates.Ready = true
	a.updatesPanel().status = desktop.updates
	a.finishBatchIfIdle(run)
	if desktop.relaunches != 0 || a.updateApplying() {
		t.Fatal("update started while the last batch results were not saved")
	}
	backend.saveRelease <- struct{}{}
	waitFor(t, a, func() bool { return len(backend.saveStarted) > 0 })
	<-backend.saveStarted
	a.applyPendingUpdate()
	if desktop.relaunches != 0 || a.updateApplying() {
		t.Fatal("update ignored the next queued batch write")
	}
	backend.saveRelease <- struct{}{}
	settle(a, tt)
	saved := backend.savedItems(batch.ID)
	if desktop.relaunches != 1 || saved[0].RecordID != "result-first" || saved[1].RecordID != "result-second" {
		t.Fatalf("update must resume after every result is durable: restarts=%d items=%+v", desktop.relaunches, saved)
	}
}

func TestBatchCloseFlushesNewestQueuedStatusWithoutUICallbacks(t *testing.T) {
	a, _, base, _ := updateFixture(t)
	batch := savedLifecycleBatch()
	backend := &lifecycleBatchBackend{batchBackend: &batchBackend{testBackend: base, batches: []domain.Batch{batch}}, saveStarted: make(chan []domain.BatchItem, 8), saveRelease: make(chan struct{})}
	t.Cleanup(func() { close(backend.saveRelease) })
	a.backend = backend
	run := runFromSaved(batch)
	a.batchRuns = []*batchRun{run}
	run.items[0].Status = batchQueued
	a.saveBatchItems(run, run.items[0])
	select {
	case <-backend.saveStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first batch write did not start")
	}
	run.items[0].Status, run.items[0].RecordID = batchDone, "newest-result"
	a.saveBatchItems(run, run.items[0])
	closed := make(chan struct{})
	go func() { a.Close(); close(closed) }()
	<-a.ctx.Done()
	select {
	case <-closed:
		t.Fatal("Close returned before the disk write finished")
	default:
	}
	backend.saveRelease <- struct{}{}
	select {
	case items := <-backend.saveStarted:
		if len(items) != 1 || items[0].RecordID != "newest-result" {
			t.Fatalf("shutdown flushed an older snapshot: %+v", items)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not flush the queued terminal status")
	}
	backend.saveRelease <- struct{}{}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited for a UI callback instead of the disk write")
	}
	if saved := backend.savedItems(batch.ID); saved[0].Status != batchDone || saved[0].RecordID != "newest-result" {
		t.Fatalf("shutdown lost the latest completed result: %+v", saved)
	}
}

func TestBatchDeleteBlocksRetryAndRestartUntilFinished(t *testing.T) {
	a, tt, base, desktop := updateFixture(t)
	batch := savedLifecycleBatch()
	batch.Items[0].Status, batch.Items[1].Status = batchFailed, batchFailed
	backend := &lifecycleBatchBackend{batchBackend: &batchBackend{testBackend: base, batches: []domain.Batch{batch}}, deleteStarted: make(chan struct{}, 8), deleteRelease: make(chan struct{})}
	t.Cleanup(func() { close(backend.deleteRelease) })
	a.backend = backend
	run := runFromSaved(batch)
	a.batchRuns = []*batchRun{run}
	a.openBatch()
	a.deleteBatch(run.key)
	select {
	case <-backend.deleteStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("batch deletion did not start")
	}
	a.retryBatch(run)
	a.deleteBatch(run.key)
	a.openBatchRun(run)
	desktop.updates.Ready = true
	a.updatesPanel().status = desktop.updates
	a.applyPendingUpdate()
	if run.running || len(backend.sent) > 0 || backend.deleteCalls.Load() != 1 || a.batch.view != "" || a.updateApplying() || desktop.relaunches != 0 {
		t.Fatal("deleting batch allowed a retry, duplicate delete, reopen or restart")
	}
	backend.deleteRelease <- struct{}{}
	settle(a, tt)
	if len(a.batchRuns) != 0 || desktop.relaunches != 1 {
		t.Fatalf("deletion did not finish before update resumed: runs=%d restarts=%d", len(a.batchRuns), desktop.relaunches)
	}
}
