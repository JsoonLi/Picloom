package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"image2ai/internal/domain"
)

func TestUpdateDraftRoundTripAndClear(t *testing.T) {
	d := &Desktop{root: t.TempDir()}
	if got, err := d.LoadUpdateDraft(); err != nil || got != nil {
		t.Fatalf("empty draft: %v %v", got, err)
	}
	want := domain.UpdateDraft{Prompt: "continue after update", References: []domain.Reference{{ID: "ref", DataURL: "image data"}}, Size: "1024x1024", Markup: "overlay", FailedJobs: []domain.UpdateFailedJob{{Args: domain.GenerateArgs{JobID: "retry", ReferenceImages: []string{}}}}}
	for _, prompt := range []string{"first save", "latest save"} {
		want.Prompt = prompt
		if err := d.SaveUpdateDraft(want); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.LoadUpdateDraft()
	if err != nil || !reflect.DeepEqual(got, &want) {
		t.Fatalf("draft: %v %v", got, err)
	}
	if err := d.ClearUpdateDraft(); err != nil {
		t.Fatal(err)
	}
	if got, err := d.LoadUpdateDraft(); err != nil || got != nil {
		t.Fatal("consumed draft returned again")
	}
}

func TestUpdateDraftRejectsCorruptionWithoutDeletingIt(t *testing.T) {
	for _, data := range []string{"broken", "null", "[]"} {
		d := &Desktop{root: t.TempDir()}
		file := filepath.Join(d.root, updateDraftFile)
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := d.LoadUpdateDraft(); err == nil {
			t.Fatal("invalid draft accepted")
		}
		if got, _ := os.ReadFile(file); string(got) != data {
			t.Fatal("corrupt draft was changed")
		}
	}
}

func TestUpdateChoicesAtomicSavePreservesOtherFields(t *testing.T) {
	root := t.TempDir()
	on := true
	want := &updateChoices{AutomaticChecks: &on, AutomaticDownloads: true, SkippedVersion: "0.7.0"}
	if err := writeUpdateFile(root, "updater.json", want); err != nil {
		t.Fatal(err)
	}
	on = false
	if err := writeUpdateFile(root, "updater.json", want); err != nil {
		t.Fatal(err)
	}
	got, err := readUpdateChoices(root)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("saved choices: %v %v", got, err)
	}
}
