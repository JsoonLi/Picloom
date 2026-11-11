package application

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

func TestChosenImageDirectoryHoldsNewWorksAndKeepsEarlierOnes(t *testing.T) {
	pixels, err := os.ReadFile(filepath.Join("..", "..", "build", "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(pixels)}}})
	}))
	defer server.Close()
	root := t.TempDir()
	store, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backend := New(store)
	if _, err = backend.SaveProviders([]domain.Provider{{ID: "local", Name: "本地", BaseURL: server.URL, APIKey: "k", Models: []string{"m"}, Resolutions: []string{"2k"}, GenerationsPath: "/images/generations", EditsPath: "/images/edits"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = backend.SaveProxy(domain.ProxyConfig{Mode: "direct"}); err != nil {
		t.Fatal(err)
	}
	args := domain.GenerateArgs{Prompt: "作品", ProviderID: "local", Model: "m", Size: "1024x1024"}
	first, err := backend.Generate(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = backend.SaveImageDirectory("relative"); err == nil {
		t.Fatal("a relative folder must be rejected")
	}
	if _, err = backend.SaveImageDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing folder must be rejected")
	}
	chosen := t.TempDir()
	canonicalChosen, err := filepath.EvalSymlinks(chosen)
	if err != nil {
		t.Fatal(err)
	}
	config, err := backend.SaveImageDirectory(chosen)
	if err != nil || config.ImageDirectory != canonicalChosen {
		t.Fatalf("choosing a folder failed: %v %q", err, config.ImageDirectory)
	}
	second, err := backend.Generate(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(second.ImagePath) != filepath.Join(canonicalChosen, "images") {
		t.Fatalf("new work must be saved in the chosen folder: %s", second.ImagePath)
	}
	other := t.TempDir()
	if config, err = backend.SaveImageDirectory(other); err != nil || len(config.PreviousImageDirectories) != 1 || config.PreviousImageDirectories[0] != canonicalChosen {
		t.Fatalf("the previous folder must be remembered: %v %+v", err, config)
	}
	for _, record := range []domain.ImageRecord{first, second} {
		got, err := backend.ReadImage(record.ID)
		if err != nil || !bytes.Equal(got, pixels) {
			t.Fatalf("earlier work unreadable after changing folders: %s %v", record.ImagePath, err)
		}
	}
	if err = backend.Delete(second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(second.ImagePath); !os.IsNotExist(err) {
		t.Fatal("deleting a work must remove its file from the earlier folder")
	}
	if config, err = backend.SaveImageDirectory(""); err != nil || config.ImageDirectory != "" {
		t.Fatalf("resetting must return to the data directory: %v", err)
	}
	if _, err = backend.ReadImage(first.ID); err != nil {
		t.Fatal(err)
	}
}
