package studio

import (
	"sync"
	"testing"
	"time"

	"image2ai/internal/domain"
)

type snapshotSaveDesktop struct {
	*automaticUpdateDesktop
	started chan struct{}
	release chan struct{}
}

func (d *snapshotSaveDesktop) SaveUpdateDraft(draft domain.UpdateDraft) error {
	d.started <- struct{}{}
	<-d.release
	return d.automaticUpdateDesktop.SaveUpdateDraft(draft)
}

func TestUpdateSnapshotRejectsNewDraftActions(t *testing.T) {
	for _, action := range []string{"import", "markup", "direct markup", "reuse"} {
		t.Run(action, func(t *testing.T) {
			a, tt, backend, desktop := updateFixture(t)
			d := &snapshotSaveDesktop{automaticUpdateDesktop: desktop, started: make(chan struct{}, 1), release: make(chan struct{})}
			a.desktop = d
			var once sync.Once
			unblock := func() { once.Do(func() { close(d.release) }) }
			t.Cleanup(func() { unblock(); a.workers.Wait() })
			a.prompt = "saved before restart"
			// A plain image preview can be open when an idle update starts.
			a.previewOpen, a.previewData = true, backend.image
			a.restartForUpdate()
			select {
			case <-d.started:
			case <-time.After(3 * time.Second):
				t.Fatal("snapshot save did not start")
			}
			switch action {
			case "import":
				a.importImages(func(int) ([]domain.Reference, error) { return nil, nil })
			case "markup":
				a.beginMarkup()
			case "direct markup":
				a.loadMarkupWithBase(func() ([]byte, error) { return backend.image, nil }, true, "")
			case "reuse":
				a.restoreGenerationDraft(domain.GenerateArgs{Prompt: "after snapshot"}, func() ([]domain.Reference, error) { return nil, nil }, nil)
			}
			if a.importing || a.markup != nil {
				t.Fatal("restart accepted a draft action after taking its snapshot")
			}
			unblock()
			settle(a, tt)
			if d.relaunches != 1 || d.saved == nil || d.saved.Prompt != "saved before restart" || a.prompt != d.saved.Prompt {
				t.Fatal("restart did not preserve its final draft snapshot")
			}
		})
	}
}
