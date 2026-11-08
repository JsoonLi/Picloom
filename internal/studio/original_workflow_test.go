package studio

import (
	"encoding/base64"
	"image2ai/internal/domain"
	"testing"
)

func TestClearInputInvalidatesPendingReferenceImport(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	started, release := make(chan struct{}), make(chan struct{})
	ref := domain.Reference{ID: "pending", Name: "pending.png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.image)}
	a.prompt = "即将清空的提示词"
	a.importImages(func(int) ([]domain.Reference, error) { close(started); <-release; return []domain.Reference{ref}, nil })
	<-started
	a.clearInput()
	close(release)
	settle(a, tt)
	if a.prompt != "" || len(a.references) != 0 || a.importing {
		t.Fatal("clear did not invalidate pending import")
	}
}
func TestCombinedModelOptionsPreserveProviderIdentity(t *testing.T) {
	a, _, _, _ := fixture(t, 1280, 900)
	a.config.Providers = []domain.Provider{{ID: "a", Name: "同名服务", Models: []string{"image-model"}}, {ID: "b", Name: "同名服务", Models: []string{"image-model"}}}
	a.providerID = "b"
	a.model = "image-model"
	labels, current, choices := a.modelChoices()
	if len(labels) != 2 || labels[0] == labels[1] || choices[current].providerID != "b" {
		t.Fatal("combined model selection lost provider identity")
	}
}
