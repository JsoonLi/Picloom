package studio

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
)

// IMAGE2AI_SCHEME_SHOTS=dir renders every color scheme for review,
// like TestNativeScreenshots.
func TestColorSchemeSnapshots(t *testing.T) {
	output := os.Getenv("IMAGE2AI_SCHEME_SHOTS")
	if output == "" {
		t.Skip("set IMAGE2AI_SCHEME_SHOTS to capture scheme renders")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	for _, id := range domain.ColorThemes {
		for _, dark := range []bool{false, true} {
			mode := "light"
			if dark {
				mode = "dark"
			}
			t.Run(id+"-"+mode, func(t *testing.T) {
				a, tt, b, _ := fixture(t, 1280, 900)
				tt.SetDark(dark)
				a.config.ColorTheme = id
				seedVisualGallery(a, tt, b)
				a.openSettings()
				a.settingsTab = "外观"
				tt.Frame()
				file, err := os.Create(filepath.Join(output, id+"-"+mode+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				if err := png.Encode(file, tt.Image()); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
