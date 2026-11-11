package studio

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

// Settings save themselves: a discrete choice (a select, a check box, a
// model chip, adding or removing a provider) saves at once, while typing
// waits for a pause. Closing the dialog saves whatever is still waiting.
const (
	settingsSaveNow   = 0
	settingsSaveTyped = 700 * time.Millisecond
)

type settingsAutosave struct {
	providers, proxy, theme, language time.Time
	saved                             bool
}

func (a *App) queueProviderSave(c *ui.Context, delay time.Duration) {
	a.autosave.providers = c.Now().Add(delay)
	a.autosave.saved = false
}

func (a *App) queueProxySave(c *ui.Context, delay time.Duration) {
	a.autosave.proxy = c.Now().Add(delay)
	a.autosave.saved = false
}

func (a *App) queueThemeSave(c *ui.Context) {
	a.autosave.theme = c.Now()
	a.autosave.saved = false
}

func (a *App) queueLanguageSave(c *ui.Context) {
	a.autosave.language = c.Now()
	a.autosave.saved = false
}

func (a *App) settingsPending() bool {
	s := a.autosave
	return !s.providers.IsZero() || !s.proxy.IsZero() || !s.theme.IsZero() || !s.language.IsZero()
}

// runSettingsSaves starts each save that has come due, one at a time, and
// asks for a frame when the next one will.
func (a *App) runSettingsSaves(c *ui.Context) {
	if a.ctx.Err() != nil || !a.settingsPending() {
		return
	}
	now := c.Now()
	flush := !a.settingsOpen
	var wait time.Duration
	due := func(at *time.Time, save func()) {
		if at.IsZero() {
			return
		}
		if a.saving {
			wait = 100 * time.Millisecond
			return
		}
		if !flush && now.Before(*at) {
			if d := at.Sub(now); wait == 0 || d < wait {
				wait = d
			}
			return
		}
		*at = time.Time{}
		save()
	}
	due(&a.autosave.theme, a.saveTheme)
	due(&a.autosave.language, a.saveLanguage)
	due(&a.autosave.proxy, a.saveProxy)
	due(&a.autosave.providers, a.autoSaveProviders)
	if wait > 0 {
		c.After(wait)
	}
}

// autoSaveProviders saves the providers once each keeps at least one model
// and one resolution; the form explains what is missing until then.
func (a *App) autoSaveProviders() {
	if a.validProviderDraft() {
		a.saveProviders()
	}
}

func (a *App) validProviderDraft() bool {
	enabled := false
	for _, p := range a.draftProviders {
		enabled = enabled || !p.Disabled
	}
	if !enabled {
		return false
	}
	for _, p := range a.draftProviders {
		if len(p.Resolutions) == 0 || len(splitModels(a.draftModels[p.ID])) == 0 {
			return false
		}
	}
	return true
}

// Track the disk operation separately from its UI callback and remote workers.
// Shutdown waits for the operation, then writes the latest pending draft last.
func (a *App) runSettingsWrite(write func() func()) {
	if a.ctx.Err() != nil {
		return
	}
	a.settingsWrites.Add(1)
	a.run(func() func() {
		defer a.settingsWrites.Done()
		done := write()
		return func() {
			if a.ctx.Err() == nil && done != nil {
				done()
			}
		}
	})
}

// Close runs on the UI thread, so drafts cannot change while they are flushed.
// Backend validation leaves the last valid configuration intact on failure.
func (a *App) flushSettingsOnClose() {
	pending := a.autosave
	a.autosave = settingsAutosave{}
	saved := func(config domain.Config, err error) {
		if err != nil {
			a.problem = err.Error()
		} else {
			a.config = config
		}
	}
	if !pending.theme.IsZero() {
		saved(a.backend.SaveTheme(a.draftTheme))
	}
	if !pending.language.IsZero() {
		saved(a.backend.SaveLanguage(a.draftLanguage))
	}
	if !pending.proxy.IsZero() && (a.draftProxy.Mode != "manual" || strings.TrimSpace(a.draftProxy.URL) != "") {
		saved(a.backend.SaveProxy(a.draftProxy))
	}
	if !pending.providers.IsZero() && a.validProviderDraft() {
		providers := cloneProviders(a.draftProviders)
		for i := range providers {
			providers[i].Models = splitModels(a.draftModels[providers[i].ID])
		}
		saved(a.backend.SaveProviders(providers))
	}
}
