package application

import (
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/storage"
)

func TestSaveBatchPreferencesValidatesAndPersists(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	b := New(store)
	out, _ := filepath.Abs("out")
	want := domain.BatchPreferences{Concurrency: 3, SizeMode: "fixed", Ratio: "4:3", OutputDirectory: out}
	if _, err := b.SaveBatchPreferences(want); err != nil {
		t.Fatal(err)
	}
	config, err := b.LoadConfig()
	if err != nil || config.Batch != want {
		t.Fatalf("saved %+v, read %+v %v", want, config.Batch, err)
	}
	for _, bad := range []domain.BatchPreferences{{Concurrency: 17}, {SizeMode: "stretch"}, {Ratio: "7:5"}, {OutputDirectory: "relative"}} {
		if _, err := b.SaveBatchPreferences(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	if config, _ := b.LoadConfig(); config.Batch != want {
		t.Fatal("rejected preferences changed the saved ones")
	}
}
