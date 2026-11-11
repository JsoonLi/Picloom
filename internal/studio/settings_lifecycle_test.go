package studio

import (
	"sync"
	"testing"
	"time"

	"image2ai/internal/domain"
	"image2ai/internal/locale"
)

type lifecycleBackend struct {
	*testBackend
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	writeMu  sync.Mutex
	calls    int
}

func (b *lifecycleBackend) SaveProviders(providers []domain.Provider) (domain.Config, error) {
	b.writeMu.Lock()
	b.calls++
	first := b.calls == 1
	b.writeMu.Unlock()
	if first {
		close(b.started)
		<-b.release
		if b.finished != nil {
			defer close(b.finished)
		}
	}
	return b.testBackend.SaveProviders(providers)
}

func settingsLifecycleApp(t *testing.T) (*App, *testBackend) {
	t.Helper()
	b := fixtureBackend()
	a := New(b, &testDesktop{})
	a.config = b.config
	a.loaded, a.started = true, true
	a.providerID, a.model = "local", "studio-image"
	a.openSettings()
	t.Cleanup(func() { a.Close(); a.workers.Wait() })
	return a, b
}

func TestSettingsReopenPreservesQueuedDraftWhileSaving(t *testing.T) {
	a, original := settingsLifecycleApp(t)
	b := &lifecycleBackend{testBackend: original, started: make(chan struct{}), release: make(chan struct{})}
	a.backend = b
	defer close(b.release)
	a.draftProvider().Name = "first save"
	a.saveProviders()
	<-b.started
	a.draftProvider().Name = "latest edit"
	a.draftProxy = domain.ProxyConfig{Mode: "manual", URL: "http://localhost:7890"}
	a.draftLanguage = locale.English
	a.autosave.providers, a.autosave.proxy, a.autosave.language = time.Now(), time.Now(), time.Now()
	a.showAPIKey = true
	a.settingsOpen = false
	a.openSettings()
	if a.draftProvider().Name != "latest edit" || a.draftProxy.URL != "http://localhost:7890" || a.draftLanguage != locale.English {
		t.Fatal("reopening during a save discarded pending settings")
	}
	if a.showAPIKey {
		t.Fatal("reopening must still hide the API key")
	}
}

func TestSettingsCloseFlushesDebouncedDrafts(t *testing.T) {
	a, b := settingsLifecycleApp(t)
	a.draftProvider().APIKey = "latest key"
	a.draftProxy = domain.ProxyConfig{Mode: "manual", URL: "http://localhost:7890"}
	a.draftTheme, a.draftLanguage = "dark", locale.English
	pending := time.Now().Add(settingsSaveTyped)
	a.autosave.providers, a.autosave.proxy, a.autosave.theme, a.autosave.language = pending, pending, pending, pending
	a.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.config.Providers[0].APIKey != "latest key" || b.config.Proxy.URL != "http://localhost:7890" || b.config.Theme != "dark" || b.config.Language != locale.English {
		t.Fatalf("shutdown discarded debounced settings: %+v", b.config)
	}
}

func TestSettingsCloseOrdersPendingDraftAfterInFlightWrite(t *testing.T) {
	a, original := settingsLifecycleApp(t)
	b := &lifecycleBackend{testBackend: original, started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	a.backend = b
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(b.release) }) }
	defer release()
	a.draftProvider().Name = "old save"
	a.saveProviders()
	<-b.started
	a.draftProvider().Name = "latest edit"
	a.autosave.providers = time.Now().Add(settingsSaveTyped)
	remoteRelease := make(chan struct{})
	defer close(remoteRelease)
	a.run(func() func() { <-remoteRelease; return nil })
	go func() {
		<-a.ctx.Done()
		// An implementation that flushes immediately after canceling must
		// not let the older delayed write commit afterward.
		time.Sleep(20 * time.Millisecond)
		release()
	}()
	// Close must cancel requests before waiting for local writes, and must
	// return even while an unrelated remote worker remains blocked.
	a.Close()
	<-b.finished
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.config.Providers[0].Name != "latest edit" {
		t.Fatalf("an older write overtook the shutdown flush: %q", b.config.Providers[0].Name)
	}
}

func TestSettingsCloseSkipsIncompleteDrafts(t *testing.T) {
	a, b := settingsLifecycleApp(t)
	a.draftProvider().Models = nil
	a.draftModels["local"] = ""
	a.draftProxy = domain.ProxyConfig{Mode: "manual"}
	a.draftLanguage = locale.English
	a.autosave.providers, a.autosave.proxy, a.autosave.language = time.Now(), time.Now(), time.Now()
	a.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.config.Providers[0].Models) == 0 || b.config.Proxy.Mode != "direct" || b.config.Language != locale.English {
		t.Fatal("shutdown must preserve valid settings while skipping incomplete drafts")
	}
}
