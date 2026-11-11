package studio

import "testing"

func TestRemovingProviderKeepsOneEnabled(t *testing.T) {
	for _, removeDisabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "last enabled", true: "disabled"}[removeDisabled], func(t *testing.T) {
			app, view, backend, _ := fixture(t, 1280, 900)
			disabled := app.config.Providers[0]
			disabled.ID, disabled.Name, disabled.Disabled = "disabled", "停用服务", true
			app.config.Providers = append(app.config.Providers, disabled)
			backend.config.Providers = cloneProviders(app.config.Providers)
			app.openSettings()
			if removeDisabled {
				app.draftProviderID = disabled.ID
			}
			app.providerDeleteOpen = true
			view.Frame()
			click(t, view, "移除提供商")
			settle(app, view)
			want := 2
			if removeDisabled {
				want = 1
			}
			if len(app.draftProviders) != want || len(backend.config.Providers) != want {
				t.Fatalf("provider removal must preserve an enabled provider and persist valid removals: draft=%d saved=%d want=%d", len(app.draftProviders), len(backend.config.Providers), want)
			}
			app.settingsOpen = false
			app.openSettings()
			if len(app.draftProviders) != want {
				t.Fatal("removed provider returned after reopening settings")
			}
		})
	}
}
