package studio

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

// workspacePixel samples the window background under the titlebar.
func workspacePixel(tt *ui.Tester) color.RGBA {
	return tt.Image().RGBAAt(200, 20)
}

func TestColorSchemesTintLightAndDark(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a, tt, _, _ := fixture(t, 1280, 900)
		tt.SetDark(dark)
		neutral := workspacePixel(tt)
		for _, id := range domain.ColorThemes {
			a.config.ColorTheme = id
			tt.Frame()
			tinted := workspacePixel(tt)
			if id == "neutral" {
				if tinted != neutral {
					t.Fatalf("neutral changed the workspace, dark=%v", dark)
				}
				continue
			}
			if tinted == neutral {
				t.Fatalf("%s left the workspace untinted, dark=%v", id, dark)
			}
		}
		// Unknown and empty values fall back to neutral.
		a.config.ColorTheme = "unknown"
		tt.Frame()
		if workspacePixel(tt) != neutral {
			t.Fatalf("unknown scheme did not fall back, dark=%v", dark)
		}
		a.config.ColorTheme = ""
		tt.Frame()
		if workspacePixel(tt) != neutral {
			t.Fatalf("empty scheme did not fall back, dark=%v", dark)
		}
	}
}

// Picking a swatch previews the scheme live, then autosaves it.
func TestColorSchemePickerAutosaves(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 1000)
	click(t, tt, "打开设置")
	a.settingsTab = "外观"
	tt.Frame()
	click(t, tt, "配色：海洋")
	if a.draftColorTheme != "ocean" {
		t.Fatalf("picker did not select ocean: %q", a.draftColorTheme)
	}
	settle(a, tt)
	b.mu.Lock()
	saved := b.config.ColorTheme
	b.mu.Unlock()
	if saved != "ocean" || a.config.ColorTheme != "ocean" {
		t.Fatalf("ocean was not autosaved: backend=%q app=%q", saved, a.config.ColorTheme)
	}
	// A second pick while a save runs is queued, not lost.
	click(t, tt, "配色：紫藤")
	settle(a, tt)
	b.mu.Lock()
	saved = b.config.ColorTheme
	b.mu.Unlock()
	if saved != "violet" {
		t.Fatalf("violet was not autosaved: %q", saved)
	}
	// Closing settings previews and keeps the saved scheme.
	click(t, tt, "关闭设置")
	settle(a, tt)
	if a.config.ColorTheme != "violet" {
		t.Fatalf("closing settings lost the scheme: %q", a.config.ColorTheme)
	}
}
