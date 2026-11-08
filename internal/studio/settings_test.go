package studio

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

func TestProviderDefaultResolutionSelectionSaveAndModelSwitch(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	a.prompt = "保留的草稿"
	a.openSettings()
	tt.Frame()
	tt.Scroll(640, 450, 0, 400)
	tt.Frame()
	click(t, tt, "提供商默认分辨率")
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	if a.draftProvider().DefaultResolution != "4k" || a.resolution != "2k" {
		t.Fatalf("selecting default must update only the settings draft: default=%q current=%q", a.draftProvider().DefaultResolution, a.resolution)
	}
	click(t, tt, "网络")
	click(t, tt, "提供商")
	click(t, tt, "保存提供商")
	settle(a, tt)
	if a.resolution != "4k" || b.config.GenerationPreferences.Resolution != "4k" || b.config.Providers[0].DefaultResolution != "4k" || a.prompt != "保留的草稿" {
		t.Fatal("saving default must persist it and apply to the current provider without resetting the prompt")
	}
	a.settingsOpen = false
	tt.Frame()
	click(t, tt, "分辨率")
	click(t, tt, "2K")
	settle(a, tt)
	a.openSettings()
	a.draftProvider().Name = "本机模拟服务"
	a.saveProviders()
	settle(a, tt)
	if a.resolution != "2k" {
		t.Fatal("unrelated settings save reset the manual resolution")
	}
	a.settingsOpen = false
	a.config.Providers = append(a.config.Providers, domain.Provider{ID: "other", Name: "另一个服务", Models: []string{"other-image"}, Resolutions: []string{"2k", "4k"}, DefaultResolution: "2k"})
	tt.Frame()
	click(t, tt, "模型")
	click(t, tt, "另一个服务 / other-image")
	settle(a, tt)
	if a.resolution != "2k" {
		t.Fatal("switching provider did not apply its default")
	}
	click(t, tt, "模型")
	click(t, tt, "本机模拟服务 / studio-image")
	settle(a, tt)
	if a.resolution != "4k" {
		t.Fatal("switching back did not restore provider default")
	}
}

func TestProviderDefaultResolutionTracksSupportedChoices(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	a.config.Providers[0].DefaultResolution = "4k"
	a.openSettings()
	tt.Frame()
	tt.Scroll(640, 450, 0, 400)
	tt.Frame()
	click(t, tt, "4K")
	if a.draftProvider().DefaultResolution != "2k" || len(a.draftProvider().Resolutions) != 1 {
		t.Fatal("removing the default resolution must select a supported replacement")
	}
	a.settingsOpen = false
	if a.config.Providers[0].DefaultResolution != "4k" {
		t.Fatal("cancel mutated saved default")
	}
}

func TestProviderDefaultResolutionPreservesRememberedSelection(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	b.mu.Lock()
	b.config.Providers[0].DefaultResolution = "4k"
	b.config.GenerationPreferences.Resolution = "2k"
	b.mu.Unlock()
	a.start()
	settle(a, tt)
	if a.resolution != "2k" {
		t.Fatal("startup must keep the last manual resolution")
	}
	a.providerID = ""
	a.reconcileSelection()
	if a.resolution != "4k" {
		t.Fatal("an initial provider choice must use its default")
	}
}

func TestSettingsModelChipsSaveSelection(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	a.openSettings()
	a.draftModelOptions["local"] = []string{"studio-image", "another-image"}
	tt.Frame()
	click(t, tt, "启用模型 studio-image")
	if len(a.draftProvider().Models) != 0 {
		t.Fatal("unchecking a model must remove it from enabled models")
	}
	click(t, tt, "保存提供商")
	if a.saving {
		t.Fatal("a provider without enabled models must not be saved")
	}
	click(t, tt, "启用模型 another-image")
	if a.draftModels["local"] != "another-image" || !contains(a.draftModelOptions["local"], "studio-image") {
		t.Fatal("checking must update the draft without removing unchecked choices")
	}
	click(t, tt, "网络")
	click(t, tt, "提供商")
	click(t, tt, "保存提供商")
	settle(a, tt)
	if len(b.config.Providers[0].Models) != 1 || b.config.Providers[0].Models[0] != "another-image" {
		t.Fatal("saving must persist only checked models")
	}
}

func TestSettingsCompactLayout(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}} {
		for _, dark := range []bool{false, true} {
			a, tt, _, _ := fixture(t, size[0], size[1])
			tt.SetDark(dark)
			a.openSettings()
			a.draftModelOptions["local"] = []string{"studio-image", "another-image", "longer-image-model-name"}
			tt.Frame()
			for _, label := range []string{"关闭设置", "编辑提供商", "Base URL", "启用模型 studio-image", "启用模型 another-image", "保存提供商"} {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.X+r.W > float32(size[0])+1 {
					t.Fatalf("%s overflows at %v, dark=%v: %+v", label, size, dark, r)
				}
			}
			save, _ := tt.Find("保存提供商")
			if save.Y < 0 || save.Y+save.H > float32(size[1]) || save.H > 36 {
				t.Fatalf("save action must stay compact and visible: %+v", save)
			}
			if size[0] == 1280 {
				field, _ := tt.Find("Base URL")
				if field.W > 310 {
					t.Fatalf("settings fields are too wide: %+v", field)
				}
			}
			if output := os.Getenv("IMAGE2AI_SETTINGS_SNAPSHOTS"); output != "" {
				if err := os.MkdirAll(output, 0700); err != nil {
					t.Fatal(err)
				}
				theme := "light"
				if dark {
					theme = "dark"
				}
				file, err := os.Create(filepath.Join(output, "settings-"+theme+"-"+fmtSize(size)+".png"))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(file, tt.Image())
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
				if size[0] < 600 {
					tt.Scroll(float32(size[0])/2, float32(size[1])/2, 0, 360)
					tt.Frame()
					file, err = os.Create(filepath.Join(output, "settings-models-"+theme+"-"+fmtSize(size)+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, tt.Image())
					file.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
}
