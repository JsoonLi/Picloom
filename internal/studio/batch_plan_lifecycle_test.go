package studio

import (
	"errors"
	"strings"
	"testing"

	"image2ai/internal/domain"
)

type providerPlanBackend struct {
	*batchBackend
}

func (b *providerPlanBackend) PlanGeneration(args domain.GenerateArgs) (domain.RequestPlan, error) {
	b.testBackend.mu.Lock()
	p := b.config.Providers[0]
	b.testBackend.mu.Unlock()
	if p.APIKey == "invalid-test-key" {
		return domain.RequestPlan{}, errors.New("invalid test credentials")
	}
	return domain.RequestPlan{Method: "POST", URL: strings.TrimRight(p.BaseURL, "/") + p.EditsPath}, nil
}

func TestBatchPreviewRefreshesAfterProviderSettingsChange(t *testing.T) {
	for _, oldFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached preview", true: "cached error"}[oldFailure], func(t *testing.T) {
			a, tt, backend, _, _ := batchFixture(t, "source.png")
			a.backend = &providerPlanBackend{batchBackend: backend}
			a.batch.prompt = "test batch edit"
			backend.testBackend.mu.Lock()
			if oldFailure {
				backend.config.Providers[0].APIKey = "invalid-test-key"
			}
			backend.testBackend.mu.Unlock()
			a.openBatch()
			settle(a, tt)
			item := a.batch.items[0]
			if oldFailure && item.PlanErr == "" || !oldFailure && item.Plan == nil {
				t.Fatal("initial provider preview was not cached")
			}
			a.batch.open = false
			a.openSettings()
			a.draftProviders[0].BaseURL = "https://changed.example/v1"
			a.draftProviders[0].EditsPath = "/custom-edits"
			a.draftProviders[0].APIKey = "valid-test-key"
			a.saveProviders()
			settle(a, tt)
			a.settingsOpen = false
			a.openBatch()
			settle(a, tt)
			plan, problem, current := a.batch.currentPlan(item)
			if !current || problem != "" || plan == nil || plan.URL != "https://changed.example/v1/custom-edits" {
				t.Fatalf("provider change kept stale preview: current=%v problem=%q plan=%+v", current, problem, plan)
			}
		})
	}
}
