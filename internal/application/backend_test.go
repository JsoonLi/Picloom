package application

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

func TestRejectedSettingsPreserveSavedConfigAndOtherFields(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend := New(store)
	if _, err = backend.SaveTheme("dark"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	checks := []func() error{
		func() error {
			_, e := backend.SaveProviders([]domain.Provider{{ID: "invalid", BaseURL: "file:///outside", Models: []string{"image"}}})
			return e
		},
		func() error {
			_, e := backend.SaveProxy(domain.ProxyConfig{Mode: "manual", URL: "http://user:secret@localhost:7890"})
			return e
		},
		func() error { _, e := backend.SaveTheme("invalid"); return e },
		func() error {
			_, e := backend.SavePreferences(domain.GenerationPreferences{Ratio: "1:1", Resolution: "8k"})
			return e
		},
	}
	for _, check := range checks {
		if err = check(); err == nil {
			t.Fatal("invalid settings accepted")
		}
		after, e := os.ReadFile(configPath)
		if e != nil || !bytes.Equal(before, after) {
			t.Fatal("rejected settings changed persisted config")
		}
	}
	cfg, err := backend.SaveProxy(domain.ProxyConfig{Mode: "direct", Bypass: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "dark" || len(cfg.Providers) == 0 {
		t.Fatal("partial settings save lost other configuration")
	}
}

func TestProviderDefaultResolutionPersistsAndRejectsUnsupported(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	b := New(store)
	config, err := b.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.Providers[0].DefaultResolution = "4k"
	if _, err = b.SaveProviders(config.Providers); err != nil {
		t.Fatal(err)
	}
	saved, err := b.LoadConfig()
	if err != nil || saved.Providers[0].DefaultResolution != "4k" {
		t.Fatalf("default was not persisted: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(store.Root(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	config.Providers[0].Resolutions = []string{"2k"}
	if _, err = b.SaveProviders(config.Providers); err == nil {
		t.Fatal("unsupported default accepted")
	}
	after, err := os.ReadFile(filepath.Join(store.Root(), "config.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected default changed saved configuration")
	}
}
