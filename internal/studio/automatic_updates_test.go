package studio

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

type automaticUpdateDesktop struct {
	*testDesktop
	installs, relaunches           int
	installErr, saveErr            error
	saved                          *domain.UpdateDraft
	installStarted, installRelease chan struct{}
}

func (d *automaticUpdateDesktop) InstallUpdate(ctx context.Context) error {
	d.updatesMu.Lock()
	d.installs++
	d.updatesMu.Unlock()
	if d.installStarted != nil {
		d.installStarted <- struct{}{}
	}
	if d.installRelease != nil {
		select {
		case <-d.installRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if d.installErr != nil {
		return d.installErr
	}
	d.updatesMu.Lock()
	d.updates.Ready = true
	d.updatesMu.Unlock()
	return nil
}

func (d *automaticUpdateDesktop) SaveUpdateDraft(draft domain.UpdateDraft) error {
	if d.saveErr != nil {
		return d.saveErr
	}
	d.saved = &draft
	return nil
}
func (d *automaticUpdateDesktop) LoadUpdateDraft() (*domain.UpdateDraft, error) { return d.saved, nil }
func (d *automaticUpdateDesktop) ClearUpdateDraft() error                       { d.saved = nil; return nil }
func (d *automaticUpdateDesktop) Relaunch()                                     { d.relaunches++ }

func updateFixture(t *testing.T) (*App, *ui.Tester, *testBackend, *automaticUpdateDesktop) {
	t.Helper()
	a, tt, b, d := fixture(t, 1280, 900)
	d.updates = domain.UpdateStatus{Version: "0.8.0", Supported: true, AutomaticChecks: true, AutomaticDownloads: true, AvailableVersion: "0.9.0"}
	up := &automaticUpdateDesktop{testDesktop: d}
	a.desktop = up
	return a, tt, b, up
}

func waitUpdateState(t *testing.T, a *App, tt *ui.Tester, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if done() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("update state did not settle")
}

func TestAutomaticUpdatesWaitForEveryGenerationAndPersistDraft(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			a, tt, b, d := updateFixture(t)
			b.generateStarted = make(chan domain.GenerateArgs, 2)
			b.generateRelease = make(chan struct{})
			for _, id := range []string{"first", "second"} {
				a.startJob(domain.GenerateArgs{JobID: id, Prompt: id, ProviderID: "local", Model: "studio-image"}, "local")
				<-b.generateStarted
			}
			a.prompt = "重启后继续编辑"
			a.references = []domain.Reference{{ID: "keep", Name: "参考图.png", DataURL: generation.DataURL(b.image, "image/png")}}
			a.checkUpdates(true)
			waitUpdateState(t, a, tt, func() bool { return !a.updates.checking })
			if d.installs != 0 || d.relaunches != 0 {
				t.Fatal("update interrupted running generations")
			}
			// Terminal progress is not permission to restart: saving may still run.
			for _, task := range a.jobs {
				task.Progress.Status = "error"
			}
			a.applyPendingUpdate()
			if a.updateApplying() {
				t.Fatal("terminal progress bypassed the active worker")
			}
			if fail {
				b.mu.Lock()
				b.fail = errors.New("generation failed")
				b.mu.Unlock()
			}
			b.generateRelease <- struct{}{}
			waitUpdateState(t, a, tt, func() bool {
				active := 0
				for _, task := range a.jobs {
					if task.running {
						active++
					}
				}
				return active == 1
			})
			if d.installs != 0 {
				t.Fatal("update did not wait for the last generation")
			}
			b.generateRelease <- struct{}{}
			settle(a, tt)
			if d.installs != 1 || d.relaunches != 1 || d.saved == nil || d.saved.Prompt != a.prompt || !reflect.DeepEqual(d.saved.References, a.references) {
				t.Fatalf("idle update must install once, save the draft, then restart: installs=%d restarts=%d draft=%+v", d.installs, d.relaunches, d.saved)
			}
			if fail && len(d.saved.FailedJobs) != 2 {
				t.Fatal("failed requests were not kept for redo")
			}
		})
	}
}

func TestAutomaticUpdatesDoNotLoopOnInstallOrDraftSaveFailure(t *testing.T) {
	for _, saveFailure := range []bool{false, true} {
		a, tt, _, d := updateFixture(t)
		if saveFailure {
			d.saveErr = errors.New("cannot save draft")
		} else {
			d.installErr = errors.New("bad signature")
		}
		a.checkUpdates(true)
		settle(a, tt)
		for i := 0; i < 10; i++ {
			tt.Frame()
		}
		if d.installs != 1 || d.relaunches != 0 || a.updates.problem == "" || a.updateApplying() {
			t.Fatal("failed update must stay usable without restarting or repeatedly installing")
		}
	}
}

func TestAutomaticUpdatesRespectPreferencesAndOpenMarkup(t *testing.T) {
	for _, mode := range []string{"checks off", "downloads off", "markup"} {
		t.Run(mode, func(t *testing.T) {
			a, tt, _, d := updateFixture(t)
			switch mode {
			case "checks off":
				d.updates.AutomaticChecks = false
			case "downloads off":
				d.updates.AutomaticDownloads = false
			case "markup":
				a.previewOpen, a.markup = true, &markupEditor{}
			}
			a.checkUpdates(true)
			settle(a, tt)
			if d.installs != 0 || d.relaunches != 0 {
				t.Fatal("automatic update bypassed the preference or editor")
			}
			if mode == "checks off" && d.updateChecks != 0 {
				t.Fatal("disabled automatic checking contacted the feed")
			}
			if mode == "markup" {
				a.previewOpen = false
				tt.Frame()
				settle(a, tt)
				if d.relaunches != 1 {
					t.Fatal("update did not resume after editing")
				}
			}
		})
	}
}

func TestUpdateInstallationBlocksNewGenerationAndDuplicateChecks(t *testing.T) {
	a, tt, b, d := updateFixture(t)
	d.installStarted, d.installRelease = make(chan struct{}, 1), make(chan struct{})
	a.checkUpdates(true)
	waitUpdateState(t, a, tt, func() bool { return a.updates.installing })
	<-d.installStarted
	a.prompt = "keep this draft"
	b.generateStarted = make(chan domain.GenerateArgs, 1)
	a.generate()
	a.submitMarkup(&markupEditor{})
	a.RefreshUpdates()
	a.checkUpdates(true)
	if len(b.generateStarted) != 0 || a.prompt != "keep this draft" || a.updates.loading {
		t.Fatal("new work raced update installation")
	}
	close(d.installRelease)
	settle(a, tt)
	if d.installs != 1 || d.relaunches != 1 {
		t.Fatal("installation or restart was duplicated")
	}
}

func TestAutomaticUpdateTimerRepeatsAndStopsOnClose(t *testing.T) {
	if automaticUpdateInterval != time.Hour || firstUpdateCheckDelay != 10*time.Second {
		t.Fatal("automatic check cadence changed")
	}
	a, tt, _, d := updateFixture(t)
	d.updates.AvailableVersion = ""
	a.startAutomaticUpdates(time.Millisecond, 20*time.Millisecond)
	waitUpdateState(t, a, tt, func() bool { d.updatesMu.Lock(); defer d.updatesMu.Unlock(); return d.updateChecks >= 2 })
	a.Close()
	settle(a, tt)
	d.updatesMu.Lock()
	before := d.updateChecks
	d.updatesMu.Unlock()
	time.Sleep(50 * time.Millisecond)
	settle(a, tt)
	if d.updateChecks != before {
		t.Fatal("timer checked after shutdown")
	}
}

func TestAutomaticRestartRestoresComposerAndFailedJobs(t *testing.T) {
	a, tt, b, d := updateFixture(t)
	a.prompt = "未提交的草稿"
	a.references = []domain.Reference{{ID: "keep", Name: "ref.png", DataURL: generation.DataURL(b.image, "image/png")}}
	a.draftSize, a.draftMarkup = "1024x1024", "saved-overlay"
	a.jobs = []*job{{Args: domain.GenerateArgs{JobID: "failed", Prompt: "retry me"}, Progress: domain.Progress{Status: "error"}}}
	want := a.updateDraft()
	a.checkUpdates(true)
	settle(a, tt)
	restored := New(b, d)
	t.Cleanup(restored.Close)
	view := ui.NewTester(restored.View, 1280, 900)
	settle(restored, view)
	if got := restored.updateDraft(); !reflect.DeepEqual(got, want) || restored.referenceThumbs["keep"] == nil || d.saved != nil {
		t.Fatalf("restart lost draft state: got=%+v want=%+v", got, want)
	}
}
