package studio

import (
	"encoding/base64"
	"image2ai/internal/domain"
	"testing"
)

func TestLegacy1KLandscapeReusePreservesAspectRatio(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	a.reuse(domain.ImageRecord{ID: "legacy-landscape", Prompt: "旧版横图", ProviderID: "local", Model: "studio-image", Size: "1360x768"})
	settle(a, tt)
	if a.ratio != "16:9" || a.resolution != "2k" {
		t.Fatalf("legacy 1K landscape became %s %s; expected 16:9 2k", a.ratio, a.resolution)
	}
}
func TestImportIgnoresInvalidReferencesOutsideRemainingSlots(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	valid := domain.Reference{ID: "new-reference", Name: "valid.png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.image)}
	a.references = []domain.Reference{{ID: "old1", DataURL: valid.DataURL}, {ID: "old2", DataURL: valid.DataURL}, {ID: "old3", DataURL: valid.DataURL}}
	a.importImages(func(remaining int) ([]domain.Reference, error) {
		if remaining != 1 {
			t.Errorf("remaining=%d", remaining)
		}
		return []domain.Reference{valid, {ID: "ignored-invalid", Name: "ignored", DataURL: "invalid"}}, nil
	})
	settle(a, tt)
	if len(a.references) != 4 || a.problem == "参考图数据无效。" {
		t.Fatalf("refs=%d error=%s", len(a.references), a.problem)
	}
}
