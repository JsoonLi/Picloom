package studio

import (
	"context"
	"testing"
	"time"

	"image2ai/internal/domain"
)

type discoveryBackend struct {
	*testBackend
	models func(context.Context, domain.Provider) ([]string, error)
	prices func(context.Context, string) ([]domain.ModelPricing, error)
}

func (b *discoveryBackend) DiscoverModels(ctx context.Context, p domain.Provider) ([]string, error) {
	return b.models(ctx, p)
}

func (b *discoveryBackend) DiscoverPricing(ctx context.Context, id string) ([]domain.ModelPricing, error) {
	return b.prices(ctx, id)
}

func discoveryApp(t *testing.T) (*App, *discoveryBackend) {
	t.Helper()
	b := &discoveryBackend{testBackend: fixtureBackend()}
	a := New(b, &testDesktop{})
	a.config = b.config
	a.config.Providers = cloneProviders(b.config.Providers)
	a.loaded, a.started = true, true
	a.providerID, a.model = "local", "studio-image"
	t.Cleanup(func() { a.Close(); a.workers.Wait() })
	return a, b
}

func applyPosted(t *testing.T, a *App) {
	t.Helper()
	select {
	case fn := <-a.pending:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("request did not post a result")
	}
}

func TestModelDiscoveryRejectsStaleDraftSource(t *testing.T) {
	for _, change := range []string{"base-url", "api-key", "closed", "reopened"} {
		t.Run(change, func(t *testing.T) {
			a, b := discoveryApp(t)
			started, release := make(chan struct{}), make(chan struct{})
			b.models = func(ctx context.Context, _ domain.Provider) ([]string, error) {
				close(started)
				select {
				case <-release:
					return []string{"obsolete-model"}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			a.openSettings()
			a.discoverModels(*a.draftProvider())
			<-started
			switch change {
			case "base-url":
				a.draftProvider().BaseURL = "http://127.0.0.1/new-service"
			case "api-key":
				a.draftProvider().APIKey = "new-test-key"
			case "closed":
				a.settingsOpen = false
			case "reopened":
				a.settingsOpen = false
				a.openSettings()
			}
			close(release)
			applyPosted(t, a)
			if a.draftModels["local"] != "studio-image" || a.draftProvider().Models[0] != "studio-image" {
				t.Fatal("obsolete model result overwrote the current draft")
			}
		})
	}
}

func TestModelDiscoveryAcceptsCurrentDraftSource(t *testing.T) {
	a, b := discoveryApp(t)
	b.models = func(context.Context, domain.Provider) ([]string, error) { return []string{"current-model"}, nil }
	a.openSettings()
	a.discoverModels(*a.draftProvider())
	applyPosted(t, a)
	if a.discoverBusy || a.draftModels["local"] != "studio-image" || !contains(a.draftModelOptions["local"], "current-model") {
		t.Fatal("current discovery result was not accepted")
	}
}

func TestModelDiscoveryPreservesEnabledModels(t *testing.T) {
	a, b := discoveryApp(t)
	b.models = func(context.Context, domain.Provider) ([]string, error) {
		return []string{"studio-image", "another-image"}, nil
	}
	a.openSettings()
	a.discoverModels(*a.draftProvider())
	applyPosted(t, a)
	if a.draftModels["local"] != "studio-image" {
		t.Fatalf("refreshing choices must preserve enabled models, got %q", a.draftModels["local"])
	}
}

func TestProviderSaveInvalidatesPendingPriceRequest(t *testing.T) {
	t.Run("automatic", func(t *testing.T) { checkPriceRequestInvalidation(t, false) })
	t.Run("manual-refresh", func(t *testing.T) { checkPriceRequestInvalidation(t, true) })
}

func checkPriceRequestInvalidation(t *testing.T, refresh bool) {
	t.Helper()
	a, b := discoveryApp(t)
	type query struct{ result chan float64 }
	started := make(chan query, 2)
	b.prices = func(ctx context.Context, _ string) ([]domain.ModelPricing, error) {
		q := query{make(chan float64)}
		started <- q
		select {
		case price := <-q.result:
			return []domain.ModelPricing{{Model: "studio-image", Currency: "USD", Prices: map[string]float64{"2k": price}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if refresh {
		a.refreshPricing("local")
	} else {
		a.loadPricing()
	}
	old := <-started
	a.openSettings()
	a.draftProvider().BaseURL = "http://127.0.0.1/new-service"
	a.saveProviders()
	applyPosted(t, a)
	var current query
	select {
	case current = <-started:
	case <-time.After(time.Second):
		t.Fatal("saving provider failed to request prices for the new configuration")
	}
	old.result <- 99
	applyPosted(t, a)
	if !a.priceLoading["local"] || len(a.pricing["local"]) != 0 {
		t.Fatal("obsolete price result modified current loading/cache state")
	}
	current.result <- 0.12
	applyPosted(t, a)
	if a.priceLoading["local"] || a.priceLabel() != "USD 0.12 / 张" {
		t.Fatalf("current price not accepted: %s", a.priceLabel())
	}
}

func TestModelDiscoveryOldResultCannotClearNewRequest(t *testing.T) {
	a, b := discoveryApp(t)
	started := make(chan chan []string, 2)
	b.models = func(ctx context.Context, _ domain.Provider) ([]string, error) {
		result := make(chan []string)
		started <- result
		select {
		case models := <-result:
			return models, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	a.openSettings()
	a.discoverModels(*a.draftProvider())
	old := <-started
	a.discoverModels(*a.draftProvider())
	current := <-started
	old <- []string{"obsolete-model"}
	applyPosted(t, a)
	if !a.discoverBusy || a.draftModels["local"] != "studio-image" {
		t.Fatal("obsolete discovery modified the current request")
	}
	current <- []string{"current-model"}
	applyPosted(t, a)
	if a.discoverBusy || a.draftModels["local"] != "studio-image" || !contains(a.draftModelOptions["local"], "current-model") {
		t.Fatal("current discovery result was not accepted")
	}
}
