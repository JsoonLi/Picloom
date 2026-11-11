package studio

import (
	"github.com/egoist/mygo/ui"
	"runtime"
)

// One native theme keeps the studio, overlays and controls on the same scale.
// High-contrast appearance remains the toolkit's own accessible appearance.
func studioTheme(c *ui.Context) {
	t := *c.Theme()
	if !c.Preferences().HighContrast {
		if t.Dark {
			t.Background = ui.Hex("#101010")
			t.Surface = ui.Hex("#242424")
			t.SurfaceHover = ui.Hex("#303030")
			t.SurfacePressed = ui.Hex("#383838")
			t.Border = ui.Hex("#3b3b3b")
			t.Text = ui.Hex("#fafafa")
			t.TextMuted = ui.Hex("#a6a6a6")
			t.Accent = ui.Hex("#eaeaea")
			t.AccentHover = ui.Hex("#ffffff")
			t.AccentPressed = ui.Hex("#d5d5d5")
			t.AccentText = ui.Hex("#1b1b1b")
			t.Selection = ui.RGBA(255, 255, 255, 0.2)
			t.Focus = ui.RGBA(240, 240, 240, 0.65)
		} else {
			t.Background = ui.Hex("#fcfcf9")
			t.Surface = ui.Hex("#ffffff")
			t.SurfaceHover = ui.Hex("#f3f2ee")
			t.SurfacePressed = ui.Hex("#e9e7e0")
			t.Border = ui.Hex("#e5e3dd")
			t.Text = ui.Hex("#292824")
			t.TextMuted = ui.Hex("#77746c")
			t.Accent = ui.Hex("#34332f")
			t.AccentHover = ui.Hex("#45443e")
			t.AccentPressed = ui.Hex("#24231f")
			t.AccentText = ui.Hex("#fafafa")
			t.Selection = ui.RGBA(40, 40, 40, 0.16)
			t.Focus = ui.RGBA(55, 55, 55, 0.55)
		}
	}
	t.Radius = 10
	t.Spacing = 4
	t.FontSize = 14 * max(float32(1), c.Preferences().TextScale)
	switch runtime.GOOS {
	case "windows":
		t.Font = "Microsoft YaHei"
	case "darwin":
		t.Font = "PingFang SC"
	}
	c.SetTheme(&t)
}
func textSize(c *ui.Context, size float32) float32 {
	return size * max(float32(1), c.Preferences().TextScale)
}

func svg(paths string) *ui.SVG {
	result, err := ui.ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
	if err != nil {
		panic(err)
	}
	return result
}

var icons = map[string]*ui.SVG{
	"search":      svg(`<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>`),
	"minimize":    svg(`<path d="M5 12h14"/>`),
	"maximize":    svg(`<rect x="5" y="5" width="14" height="14" rx="1"/>`),
	"star":        svg(`<path d="m12 3 2.8 5.7 6.3.9-4.5 4.4 1 6.2-5.6-2.9-5.6 2.9 1-6.2L3 9.6l6.2-.9z"/>`),
	"star-filled": svg(`<path fill="currentColor" d="m12 3 2.8 5.7 6.3.9-4.5 4.4 1 6.2-5.6-2.9-5.6 2.9 1-6.2L3 9.6l6.2-.9z"/>`),
	"download":    svg(`<path d="M12 3v12m-5-5 5 5 5-5M5 17v4h14v-4"/>`),
	"settings":    svg(`<path d="m9.2 3-.7 2.3-2 .9-2.2-.6-1.6 2.8 1.6 1.7-.2 2.2-1.6 1.8 1.7 2.8 2.2-.6 2 .9.7 2.3h3.3l.7-2.3 2-.9 2.2.6 1.7-2.8-1.6-1.8-.2-2.2 1.6-1.7-1.6-2.8-2.2.6-2-.9-.7-2.3z"/><circle cx="11.7" cy="11" r="3"/>`),
	"plus":        svg(`<path d="M12 5v14M5 12h14"/>`),
	"clipboard":   svg(`<rect x="5" y="5" width="14" height="16" rx="2"/><rect x="9" y="3" width="6" height="4" rx="1"/>`),
	"copy":        svg(`<rect x="8" y="8" width="13" height="13" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>`),
	"image":       svg(`<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="m21 15-5-5L5 21"/>`),
	"images":      svg(`<path d="M4 15H3a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1h11a1 1 0 0 1 1 1v1"/><rect x="7" y="7" width="15" height="15" rx="2"/><circle cx="11.5" cy="11.5" r="1.5"/><path d="m22 18-5-5-8 9"/>`),
	"grid":        svg(`<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>`),
	"refresh":     svg(`<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 6a8 8 0 0 1 13 4M5 14a8 8 0 0 0 13 4"/>`),
	"heart":       svg(`<path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8z"/>`),
	"trash":       svg(`<path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7"/>`),
	"expand":      svg(`<path d="M8 3H3v5M16 3h5v5M21 16v5h-5M8 21H3v-5"/>`),
	"more":        svg(`<circle cx="5" cy="12" r="1.2" fill="currentColor"/><circle cx="12" cy="12" r="1.2" fill="currentColor"/><circle cx="19" cy="12" r="1.2" fill="currentColor"/>`),
	"eye":         svg(`<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="2.8"/>`),
	"eye-off":     svg(`<path d="M10.6 5.6A9.6 9.6 0 0 1 12 5.5c6 0 9.5 6.5 9.5 6.5a16 16 0 0 1-2.9 3.6M6.5 6.9C3.9 8.6 2.5 12 2.5 12S6 18.5 12 18.5a9 9 0 0 0 4.6-1.3M9.9 9.9a2.8 2.8 0 0 0 4.2 4.2M3 3l18 18"/>`),
	"close":       svg(`<path d="m6 6 12 12M6 18 18 6"/>`),
	"arrow":       svg(`<path d="M5 12h14m-6-6 6 6-6 6"/>`),
}
var studioMark = svg(`<circle cx="12" cy="12" r="9"/><path d="m16.4 4.2 1.2 10.1M20.6 11.7l-8.2 6.5M16.1 20l-9.8-3.9M7.1 19.8l-1.3-10M3.2 11.5l8.1-6.1M7.9 4.2l9.6 3.9"/>`)

func iconButton(c *ui.Context, key, label, name string) ui.Element {
	b := ui.Button(c.Key(key), "").Label(label).Tooltip(label).Size(40, 40).Padding(0).Border(0, ui.Transparent).Background(ui.Transparent).Radius(8)
	if b.Hovered() {
		b.Background(c.Theme().SurfaceHover).TextColor(c.Theme().Text)
	}
	if b.Pressed() {
		b.Background(c.Theme().SurfacePressed)
	}
	b.Children(func() { ui.Icon(c, icons[name]).Size(16, 16) })
	return b
}
func quietButton(c *ui.Context, key, label string) ui.Element {
	b := ui.Button(c.Key(key), label).Height(36).Padding(8, 12).Border(0, ui.Transparent).Background(ui.Transparent)
	if b.Hovered() {
		b.Background(c.Theme().SurfaceHover)
	}
	if b.Pressed() {
		b.Background(c.Theme().SurfacePressed)
	}
	return b
}

// Shared native paper surface for the composer, artwork and supporting panes.
// The faint contact shadow leaves imagery and typography in the foreground.
func glassPane(c *ui.Context, key string) ui.Element {
	pane := ui.Column(c.Key(key)).Background(c.Theme().Surface).Border(1, c.Theme().Border)
	if !c.Preferences().HighContrast {
		if c.Theme().Dark {
			pane.Shadow(0, 3, 10, -4, ui.RGBA(0, 0, 0, 0.24)).InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, 0.13))
		} else {
			pane.Shadow(0, 2, 8, -3, ui.RGBA(45, 42, 35, 0.06))
		}
	}
	return pane
}
func toolbarColor(c *ui.Context) ui.Color {
	if c.Preferences().HighContrast {
		return c.Theme().Surface
	}
	if c.Theme().Dark {
		return ui.Hex("#202020")
	}
	return ui.Hex("#f7f6f2")
}

func popoverTheme(c *ui.Context) func() {
	old := c.Theme()
	panel := *old
	panel.Background = old.Surface
	c.SetTheme(&panel)
	return func() { c.SetTheme(old) }
}
