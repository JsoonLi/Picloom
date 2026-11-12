package studio

import (
	"github.com/egoist/mygo/ui"
	"runtime"
)

// paletteVariant is one appearance of a color scheme; schemes only tint
// these surfaces, danger, warning and success stay the toolkit's own.
type paletteVariant struct {
	background, surface, surfaceHover, surfacePressed, border string
	text, textMuted                                           string
	accent, accentHover, accentPressed, accentText            string
}

type palette struct {
	light, dark paletteVariant
}

// palettes hold every selectable color scheme, keyed by the ids in
// domain.ColorThemes.
var palettes = map[string]palette{
	"neutral": {
		light: paletteVariant{background: "#fcfcf9", surface: "#ffffff", surfaceHover: "#f3f2ee", surfacePressed: "#e9e7e0", border: "#e5e3dd", text: "#292824", textMuted: "#77746c", accent: "#34332f", accentHover: "#45443e", accentPressed: "#24231f", accentText: "#fafafa"},
		dark:  paletteVariant{background: "#101010", surface: "#242424", surfaceHover: "#303030", surfacePressed: "#383838", border: "#3b3b3b", text: "#fafafa", textMuted: "#a6a6a6", accent: "#eaeaea", accentHover: "#ffffff", accentPressed: "#d5d5d5", accentText: "#1b1b1b"}},
	"ocean": {
		light: paletteVariant{background: "#f8fafb", surface: "#ffffff", surfaceHover: "#eef3f5", surfacePressed: "#e3eaee", border: "#dde4e8", text: "#26333c", textMuted: "#6d7c86", accent: "#1c6f96", accentHover: "#2c7ea6", accentPressed: "#155a7c", accentText: "#f5fafc"},
		dark:  paletteVariant{background: "#0f1517", surface: "#222b2f", surfaceHover: "#2d373c", surfacePressed: "#354146", border: "#384349", text: "#f4f8f9", textMuted: "#a2b0b6", accent: "#79c4e0", accentHover: "#8fd3ec", accentPressed: "#61b1d0", accentText: "#10242e"}},
	"forest": {
		light: paletteVariant{background: "#f9faf7", surface: "#ffffff", surfaceHover: "#eff2eb", surfacePressed: "#e4e9df", border: "#dfe4d9", text: "#28302a", textMuted: "#707a6f", accent: "#4d7c4a", accentHover: "#5d8c5a", accentPressed: "#3f6b3c", accentText: "#f7faf5"},
		dark:  paletteVariant{background: "#111512", surface: "#232b25", surfaceHover: "#2e3731", surfacePressed: "#364139", border: "#39443c", text: "#f5f8f5", textMuted: "#a4b1a6", accent: "#93cf9b", accentHover: "#a6dcad", accentPressed: "#7fbf88", accentText: "#14251a"}},
	"sunset": {
		light: paletteVariant{background: "#fcfaf6", surface: "#ffffff", surfaceHover: "#f5f0e9", surfacePressed: "#ebe5da", border: "#e7e0d5", text: "#322d26", textMuted: "#7d7467", accent: "#c2570a", accentHover: "#d1661c", accentPressed: "#a84a05", accentText: "#fdf8f2"},
		dark:  paletteVariant{background: "#141210", surface: "#262320", surfaceHover: "#312d29", surfacePressed: "#393531", border: "#413d37", text: "#faf6f1", textMuted: "#ada59a", accent: "#eda668", accentHover: "#f6b47c", accentPressed: "#e09355", accentText: "#2a1c0e"}},
	"rose": {
		light: paletteVariant{background: "#fcfafb", surface: "#ffffff", surfaceHover: "#f5eff1", surfacePressed: "#ebe4e7", border: "#e6dde0", text: "#2f292c", textMuted: "#7a6f74", accent: "#bf3f66", accentHover: "#cf4f76", accentPressed: "#a63154", accentText: "#fdf5f7"},
		dark:  paletteVariant{background: "#141113", surface: "#262225", surfaceHover: "#312c30", surfacePressed: "#393438", border: "#403a3e", text: "#faf6f7", textMuted: "#aca2a7", accent: "#ec8dab", accentHover: "#f59ebb", accentPressed: "#de7a99", accentText: "#2c121d"}},
	"violet": {
		light: paletteVariant{background: "#faf9fb", surface: "#ffffff", surfaceHover: "#f1eef4", surfacePressed: "#e6e2ec", border: "#e2dee7", text: "#2c2831", textMuted: "#746e7d", accent: "#6b52a3", accentHover: "#7a61b0", accentPressed: "#5a448f", accentText: "#f8f6fc"},
		dark:  paletteVariant{background: "#121014", surface: "#252229", surfaceHover: "#302c35", surfacePressed: "#38343d", border: "#3e3a44", text: "#f7f5fa", textMuted: "#a7a1b0", accent: "#b7a0e2", accentHover: "#c5b0ec", accentPressed: "#a68fd8", accentText: "#1e1531"}},
}

// paletteFor resolves the saved or draft scheme, unknown values falling
// back to the neutral default.
func paletteFor(id string) palette {
	if p, ok := palettes[id]; ok {
		return p
	}
	return palettes["neutral"]
}

// accent returns the scheme's accent for the current appearance; the
// settings picker uses it for the preview dot.
func (p palette) accent(dark bool) ui.Color {
	if dark {
		return ui.Hex(p.dark.accent)
	}
	return ui.Hex(p.light.accent)
}

// One native theme keeps the studio, overlays and controls on the same scale.
// The chosen color scheme tints every surface; high-contrast appearance
// remains the toolkit's own accessible appearance.
func (a *App) studioTheme(c *ui.Context) {
	t := *c.Theme()
	if !c.Preferences().HighContrast {
		id := a.config.ColorTheme
		if a.settingsOpen {
			id = a.draftColorTheme
		}
		v := paletteFor(id).light
		if t.Dark {
			v = paletteFor(id).dark
		}
		t.Background = ui.Hex(v.background)
		t.Surface = ui.Hex(v.surface)
		t.SurfaceHover = ui.Hex(v.surfaceHover)
		t.SurfacePressed = ui.Hex(v.surfacePressed)
		t.Border = ui.Hex(v.border)
		t.Text = ui.Hex(v.text)
		t.TextMuted = ui.Hex(v.textMuted)
		t.Accent = ui.Hex(v.accent)
		t.AccentHover = ui.Hex(v.accentHover)
		t.AccentPressed = ui.Hex(v.accentPressed)
		t.AccentText = ui.Hex(v.accentText)
		t.Selection = t.Accent.Alpha(0.16)
		t.Focus = t.Accent.Alpha(0.55)
		if t.Dark {
			t.Selection = t.Accent.Alpha(0.2)
			t.Focus = t.Accent.Alpha(0.65)
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

// toolbarColor is a half-step between the window and its surfaces,
// derived from the scheme's own colors so every palette stays tinted.
func toolbarColor(c *ui.Context) ui.Color {
	if c.Preferences().HighContrast {
		return c.Theme().Surface
	}
	t := c.Theme()
	return t.Background.Mix(t.Border, 0.35)
}

func popoverTheme(c *ui.Context) func() {
	old := c.Theme()
	panel := *old
	panel.Background = old.Surface
	c.SetTheme(&panel)
	return func() { c.SetTheme(old) }
}
