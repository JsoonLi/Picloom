package studio

import (
	"time"

	"image2ai/internal/locale"
)

const automaticUpdateInterval = time.Hour
const firstUpdateCheckDelay = 10 * time.Second

// StartAutomaticUpdates uses a real timer so minimized windows still check.
// All decisions about generation and restarting remain on the UI thread.
func (a *App) StartAutomaticUpdates() {
	a.startAutomaticUpdates(firstUpdateCheckDelay, automaticUpdateInterval)
}

func (a *App) startAutomaticUpdates(first, interval time.Duration) {
	go func() {
		timer := time.NewTimer(first)
		defer timer.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-timer.C:
				a.post(func() { a.checkUpdates(true) })
				timer.Reset(interval)
			}
		}
	}()
}

func (a *App) ResumeAutomaticUpdates() {
	a.post(func() {
		if a.updates == nil || time.Since(a.updates.status.LastCheck) >= automaticUpdateInterval {
			a.checkUpdates(true)
		}
	})
}

func (a *App) OpenUpdateSettings() {
	a.openSettings()
	a.settingsTab = "更新"
	a.checkUpdates(false)
}

func (a *App) checkUpdates(automatic bool) {
	panel := a.updatesPanel()
	if panel.loading || panel.saving || panel.checking || panel.installing || panel.restarting || a.ctx.Err() != nil {
		return
	}
	panel.checking = true
	panel.problem = ""
	a.run(func() func() {
		status := a.desktop.UpdateStatus()
		var err error
		if status.Supported && (!automatic || status.AutomaticChecks) {
			err = a.desktop.CheckForUpdates()
			status = a.desktop.UpdateStatus()
		}
		return func() {
			panel.checking = false
			panel.status = status
			if err != nil {
				panel.problem = err.Error()
				return
			}
			panel.attemptedVersion, panel.restartFailed = "", false
			if !automatic && status.Supported && status.AvailableVersion == "" {
				a.showNotice(locale.T("当前已是最新版本。"))
			}
			a.applyPendingUpdate()
		}
	})
}

func (a *App) updateApplying() bool {
	return a.updates != nil && (a.updates.installing || a.updates.restarting)
}

func (a *App) updateRestarting() bool {
	return a.updates != nil && a.updates.restarting
}

func (a *App) updateBusy() bool {
	if !a.loaded || a.updateRestoreFailed || a.importing || a.saving || a.settingsPending() || a.preferencesSaving || a.batchBusy() || a.batchSaving || len(a.batchSaves) > 0 {
		return true
	}
	for _, run := range a.batchRuns {
		if run.deleting {
			return true
		}
	}
	// An editor may hold unsaved strokes or be preparing a markup request.
	if a.previewOpen && a.markup != nil {
		return true
	}
	for _, task := range a.jobs {
		// A terminal progress event can arrive before Generate saves its result.
		if task.running || task.Progress.Status != "error" {
			return true
		}
	}
	return false
}

func (a *App) applyPendingUpdate() {
	panel := a.updates
	if panel == nil || panel.loading || panel.saving || panel.checking || panel.problem != "" || a.updateApplying() || a.ctx.Err() != nil {
		return
	}
	status := panel.status
	if !status.Supported || status.AvailableVersion == "" ||
		(!(status.AutomaticChecks && status.AutomaticDownloads) && !panel.installRequested) || a.updateBusy() {
		return
	}
	if status.Ready {
		if !panel.restartFailed {
			a.restartForUpdate()
		}
		return
	}
	if panel.attemptedVersion == status.AvailableVersion {
		return
	}
	panel.attemptedVersion = status.AvailableVersion
	panel.installing = true
	panel.problem = ""
	a.run(func() func() {
		err := a.desktop.InstallUpdate(a.ctx)
		status := a.desktop.UpdateStatus()
		return func() {
			panel.installing = false
			panel.status = status
			if err != nil {
				panel.problem = err.Error()
				return
			}
			a.applyPendingUpdate()
		}
	})
}

func (a *App) restartForUpdate() {
	panel := a.updatesPanel()
	panel.restarting = true
	draft := a.updateDraft()
	a.run(func() func() {
		err := a.desktop.SaveUpdateDraft(draft)
		return func() {
			if a.ctx.Err() != nil {
				return
			}
			if err != nil {
				panel.restarting, panel.restartFailed = false, true
				panel.problem = err.Error()
				return
			}
			a.desktop.Relaunch()
		}
	})
}
