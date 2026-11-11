package studio

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/locale"
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
	settle(a, tt)
	if a.resolution != "4k" || b.config.GenerationPreferences.Resolution != "4k" || b.config.Providers[0].DefaultResolution != "4k" || a.prompt != "保留的草稿" {
		t.Fatal("choosing a default must save it at once and apply it to the current provider without resetting the prompt")
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
	a, tt, b, _ := fixture(t, 1280, 900)
	a.config.Providers[0].DefaultResolution = "4k"
	a.openSettings()
	tt.Frame()
	tt.Scroll(640, 450, 0, 400)
	tt.Frame()
	click(t, tt, "4K")
	if a.draftProvider().DefaultResolution != "2k" || len(a.draftProvider().Resolutions) != 1 {
		t.Fatal("removing the default resolution must select a supported replacement")
	}
	settle(a, tt)
	if b.config.Providers[0].DefaultResolution != "2k" || len(b.config.Providers[0].Resolutions) != 1 {
		t.Fatal("changing supported resolutions must save the replacement default")
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
	settle(a, tt)
	if len(b.config.Providers[0].Models) != 1 || b.config.Providers[0].Models[0] != "studio-image" {
		t.Fatal("a provider without enabled models must not be saved")
	}
	click(t, tt, "启用模型 another-image")
	if a.draftModels["local"] != "another-image" || !contains(a.draftModelOptions["local"], "studio-image") {
		t.Fatal("checking must update the draft without removing unchecked choices")
	}
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
			for _, label := range []string{"关闭设置", "编辑提供商", "Base URL", "启用模型 studio-image", "启用模型 another-image", "新增提供商"} {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.X+r.W > float32(size[0])+1 {
					t.Fatalf("%s overflows at %v, dark=%v: %+v", label, size, dark, r)
				}
			}
			if tt.HasText("保存提供商") {
				t.Fatal("settings save themselves; no save button may remain")
			}
			add, _ := tt.Find("新增提供商")
			if add.Y < 0 || add.Y+add.H > float32(size[1]) {
				t.Fatalf("adding a provider must stay visible: %+v", add)
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

func TestSettingsAutosaveWaitsForTypingAndFlushesOnClose(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	a.openSettings()
	tt.Frame()
	click(t, tt, "提供商名称")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("新服务")
	settle(a, tt)
	if b.config.Providers[0].Name != "本机模拟服务" {
		t.Fatal("typing must wait for a pause before saving")
	}
	tt.Advance(settingsSaveTyped)
	settle(a, tt)
	if b.config.Providers[0].Name != "新服务" || !tt.HasText("已自动保存") {
		t.Fatalf("a pause in typing must save the provider: %q", b.config.Providers[0].Name)
	}
	click(t, tt, "Base URL")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("http://127.0.0.1:9000")
	click(t, tt, "关闭设置")
	settle(a, tt)
	if b.config.Providers[0].BaseURL != "http://127.0.0.1:9000" {
		t.Fatal("closing settings must save what is still waiting")
	}
	a.openSettings()
	a.settingsTab = "网络"
	tt.Frame()
	click(t, tt, "选择连接方式")
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	settle(a, tt)
	if b.config.Proxy.Mode != "direct" {
		t.Fatal("choosing a manual proxy without an address must not save an invalid proxy")
	}
	click(t, tt, "代理地址")
	tt.Type("http://127.0.0.1:7890")
	tt.Advance(settingsSaveTyped)
	settle(a, tt)
	if b.config.Proxy.Mode != "manual" || b.config.Proxy.URL != "http://127.0.0.1:7890" {
		t.Fatalf("proxy address must save after typing: %+v", b.config.Proxy)
	}
}

func TestSettingsRevealAPIKeyAndChooseImageDirectory(t *testing.T) {
	a, tt, b, d := fixture(t, 1280, 900)
	a.config.Providers[0].APIKey = "secret-key"
	a.openSettings()
	tt.Frame()
	if tt.HasText("管理模型服务、网络与外观") {
		t.Fatal("the settings title must stand alone")
	}
	if a.showAPIKey {
		t.Fatal("the API key must start hidden")
	}
	click(t, tt, "显示 API Key")
	if !a.showAPIKey {
		t.Fatal("showing the API key must reveal it")
	}
	click(t, tt, "隐藏 API Key")
	if a.showAPIKey {
		t.Fatal("hiding the API key must mask it again")
	}
	a.showAPIKey = true
	a.settingsOpen = false
	a.openSettings()
	if a.showAPIKey {
		t.Fatal("reopening settings must hide the API key again")
	}
	tt.Frame()
	click(t, tt, "外观")
	d.chosenDirectory = filepath.Join("D:", "Picloom 作品")
	click(t, tt, "选择作品保存位置")
	settle(a, tt)
	if b.config.ImageDirectory != d.chosenDirectory || !tt.HasText("恢复默认") {
		t.Fatalf("choosing a folder must save it: %q", b.config.ImageDirectory)
	}
	click(t, tt, "恢复默认")
	settle(a, tt)
	if b.config.ImageDirectory != "" {
		t.Fatal("resetting must return works to the data directory")
	}
}

func TestProviderSwitchHidesModelsAndKeepsOneEnabled(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	b.mu.Lock()
	b.config.Providers = append(b.config.Providers, domain.Provider{ID: "other", Name: "另一个服务", BaseURL: "http://127.0.0.1", APIKey: "k", Models: []string{"other-image"}, Resolutions: []string{"2k"}})
	b.mu.Unlock()
	a.config.Providers = append(a.config.Providers, b.config.Providers[1])
	a.openSettings()
	a.draftProviderID = "other"
	tt.Frame()
	click(t, tt, "停用提供商")
	settle(a, tt)
	if !b.config.Providers[1].Disabled || !tt.HasText("已停用") {
		t.Fatal("switching a provider off must save it at once")
	}
	labels, _, _ := a.modelChoices()
	for _, label := range labels {
		if strings.Contains(label, "other-image") {
			t.Fatal("a disabled provider's models must leave the model list")
		}
	}
	a.draftProviderID = "local"
	tt.Frame()
	click(t, tt, "停用提供商")
	settle(a, tt)
	if b.config.Providers[0].Disabled {
		t.Fatal("the last enabled provider must stay on")
	}
	a.draftProviderID = "other"
	tt.Frame()
	click(t, tt, "启用提供商")
	settle(a, tt)
	if b.config.Providers[1].Disabled {
		t.Fatal("switching a provider back on must save it")
	}
	a.settingsOpen = false
	a.providerID, a.model = "other", "other-image"
	a.config.Providers[1].Disabled = true
	a.reconcileSelection()
	if a.providerID != "local" || a.model != "studio-image" {
		t.Fatalf("a disabled selection must move to an enabled provider: %s %s", a.providerID, a.model)
	}
}

func TestLanguageSwitchTranslatesAtOnceAndSaves(t *testing.T) {
	a, tt, b, d := fixture(t, 1280, 900)
	t.Cleanup(func() { locale.Set(locale.Chinese) })
	a.openSettings()
	a.settingsTab = "外观"
	tt.Frame()
	click(t, tt, "选择语言")
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	settle(a, tt)
	if d.language != locale.English || b.config.Language != locale.English {
		t.Fatalf("choosing English must apply and save it: %q %q", d.language, b.config.Language)
	}
	if !tt.HasText("Appearance") || !tt.HasText("Language") || tt.HasText("外观") {
		t.Fatal("the settings must switch to English at once")
	}
	click(t, tt, "Choose language")
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyEnter)
	settle(a, tt)
	if b.config.Language != "system" || !tt.HasText("外观") {
		t.Fatalf("following the system must save it: %q", b.config.Language)
	}
}
