package application

import (
	"strings"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

func TestPreferencesUseProviderUTF16Limits(t *testing.T) {
	for _, value := range []string{strings.Repeat("模", 100), strings.Repeat("🌟", 100)} {
		t.Run(string([]rune(value)[:1]), func(t *testing.T) {
			store, err := storage.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			b := New(store)
			provider := domain.Provider{ID: value, Name: "测试服务", BaseURL: "http://127.0.0.1", Models: []string{value}, Resolutions: []string{"2k"}}
			if _, err := b.SaveProviders([]domain.Provider{provider}); err != nil {
				t.Fatalf("valid provider rejected: %v", err)
			}
			prefs := domain.GenerationPreferences{ProviderID: value, Model: value, Ratio: "1:1", Resolution: "2k"}
			if _, err := b.SavePreferences(prefs); err != nil {
				t.Fatalf("configured provider/model cannot be selected: %v", err)
			}
			saved, err := b.LoadConfig()
			if err != nil || saved.GenerationPreferences != prefs {
				t.Fatalf("selection not persisted: %+v %v", saved.GenerationPreferences, err)
			}
			prefs.Model = strings.Repeat("🌟", 101)
			if _, err := b.SavePreferences(prefs); err == nil {
				t.Fatal("model longer than 200 UTF-16 units accepted")
			}
			prefs.Model, prefs.ProviderID = value, strings.Repeat("a", 201)
			if _, err := b.SavePreferences(prefs); err == nil {
				t.Fatal("provider ID longer than 200 UTF-16 units accepted")
			}
		})
	}
}
