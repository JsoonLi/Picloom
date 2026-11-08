package studio

import (
	"sync"
	"testing"
	"time"

	"image2ai/internal/domain"
)

type preferenceWriteBackend struct {
	*testBackend
	firstStarted chan struct{}
	firstRelease chan struct{}
	nextStarted  chan struct{}
	mu           sync.Mutex
	calls        int
}

func (b *preferenceWriteBackend) SavePreferences(p domain.GenerationPreferences) (domain.Config, error) {
	b.mu.Lock()
	b.calls++
	call := b.calls
	b.mu.Unlock()
	if call == 1 {
		close(b.firstStarted)
		<-b.firstRelease
	} else {
		select {
		case b.nextStarted <- struct{}{}:
		default:
		}
	}
	return b.testBackend.SavePreferences(p)
}

func TestPreferenceWritesKeepLatestUISelection(t *testing.T) {
	b := &preferenceWriteBackend{testBackend: fixtureBackend(), firstStarted: make(chan struct{}), firstRelease: make(chan struct{}), nextStarted: make(chan struct{}, 2)}
	a := New(b, &testDesktop{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(b.firstRelease)
		}
		a.Close()
		a.workers.Wait()
	})
	a.providerID, a.model, a.ratio, a.resolution = "local", "studio-image", "3:4", "2k"
	a.savePreferences()
	select {
	case <-b.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first preference save did not start")
	}
	a.ratio = "16:9"
	a.savePreferences()
	a.ratio = "21:9"
	a.savePreferences()
	// The first disk write is deliberately blocked. A later UI event must
	// not reach the backend ahead of it and then be overwritten by it.
	concurrent := false
	select {
	case <-b.nextStarted:
		concurrent = true
	case <-time.After(50 * time.Millisecond):
	}
	close(b.firstRelease)
	released = true
	for range 3 {
		a.workers.Wait()
		a.drain()
	}
	b.testBackend.mu.Lock()
	saved := b.config.GenerationPreferences.Ratio
	b.testBackend.mu.Unlock()
	if concurrent || saved != "21:9" {
		t.Fatalf("preference writes bypassed UI order: concurrent=%v saved ratio=%q", concurrent, saved)
	}
	b.mu.Lock()
	calls := b.calls
	b.mu.Unlock()
	if calls != 2 {
		t.Fatalf("queued preference changes were not coalesced: %d writes", calls)
	}
}

func TestPreferenceQueueDoesNotStartAfterClose(t *testing.T) {
	b := &preferenceWriteBackend{testBackend: fixtureBackend(), firstStarted: make(chan struct{}), firstRelease: make(chan struct{}), nextStarted: make(chan struct{}, 2)}
	a := New(b, &testDesktop{})
	a.providerID, a.model = "local", "studio-image"
	a.savePreferences()
	<-b.firstStarted
	a.ratio = "16:9"
	a.savePreferences()
	a.Close()
	close(b.firstRelease)
	a.workers.Wait()
	a.drain()
	a.workers.Wait()
	b.mu.Lock()
	calls := b.calls
	b.mu.Unlock()
	if calls != 1 {
		t.Fatalf("closing the app started a queued preference write: %d calls", calls)
	}
}
