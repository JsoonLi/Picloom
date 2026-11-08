package studio

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"image2ai/internal/domain"
)

type failingUpdateDesktop struct{ domain.Desktop }

func (d failingUpdateDesktop) SetUpdatePreferences(bool, bool) (domain.UpdateStatus, error) {
	return d.UpdateStatus(), errors.New("无法保存更新设置")
}

func TestUpdatePreferenceErrorSurvivesChangeNotification(t *testing.T) {
	a, tt, _, d := fixture(t, 740, 560)
	d.updates = domain.UpdateStatus{Supported: true, AutomaticChecks: true}
	a.desktop = failingUpdateDesktop{d}
	a.RefreshUpdates()
	settle(a, tt)
	a.saveUpdatePreferences(true, true)
	// The native updater sends this notification while its setters save.
	a.RefreshUpdates()
	settle(a, tt)
	if a.updatesPanel().problem != "无法保存更新设置" {
		t.Fatal("change notification hid the save error")
	}
}

func TestUpdateSettingsPreserveDraftAndPersistPreferences(t *testing.T) {
	a, tt, _, desktop := fixture(t, 1280, 900)
	desktop.updates = domain.UpdateStatus{Version: "0.6.0", Supported: true, AutomaticChecks: true, AutomaticDownloads: true, LastCheck: time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC)}
	a.prompt = "继续保留的草稿"
	a.references = []domain.Reference{{ID: "keep", Name: "参考图.png"}}
	a.openSettings()
	tt.Frame()
	click(t, tt, "更新")
	settle(a, tt)
	if !tt.HasText("当前版本 0.6.0") {
		t.Fatal("current version missing")
	}
	click(t, tt, "自动下载并安装更新")
	settle(a, tt)
	if desktop.UpdateStatus().AutomaticDownloads {
		t.Fatal("automatic install preference not saved")
	}
	click(t, tt, "检查软件更新")
	settle(a, tt)
	if desktop.updateChecks != 1 {
		t.Fatal("manual check did not reach desktop capability")
	}
	a.settingsOpen = false
	tt.Frame()
	if a.prompt != "继续保留的草稿" || len(a.references) != 1 {
		t.Fatal("update settings reset studio draft")
	}
}

func TestUpdateSettingsFitNarrowAndDesktopWindows(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}, {320, 768}} {
		for _, dark := range []bool{false, true} {
			a, tt, _, d := fixture(t, size[0], size[1])
			tt.SetDark(dark)
			d.updates = domain.UpdateStatus{Version: "0.6.0", Supported: true, AutomaticChecks: true, AutomaticDownloads: true}
			a.openSettings()
			tt.Frame()
			click(t, tt, "更新")
			settle(a, tt)
			for _, label := range []string{"更新", "检查软件更新", "自动检查更新", "自动下载并安装更新"} {
				r, ok := tt.Find(label)
				if !ok || r.X < 0 || r.X+r.W > float32(size[0])+1 {
					t.Fatalf("%s overflows at %v: %+v", label, size, r)
				}
			}
			button, _ := tt.Find("检查软件更新")
			if button.Y < 0 || button.Y+button.H > float32(size[1]) {
				t.Fatal("check button must remain visible")
			}
			if output := os.Getenv("IMAGE2AI_UPDATE_SNAPSHOTS"); output != "" {
				if err := os.MkdirAll(output, 0700); err != nil {
					t.Fatal(err)
				}
				theme := "light"
				if dark {
					theme = "dark"
				}
				file, err := os.Create(filepath.Join(output, "updates-"+theme+"-"+fmtSize(size)+".png"))
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
