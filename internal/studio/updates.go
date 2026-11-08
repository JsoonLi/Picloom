package studio

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

type updatePanel struct {
	status          domain.UpdateStatus
	loading, saving bool
	refreshPending  bool
	request         uint64
	problem         string
}

func (a *App) updatesPanel() *updatePanel {
	if a.updates == nil {
		a.updates = &updatePanel{}
	}
	return a.updates
}

// RefreshUpdates is called on the main thread; all filesystem probes run in
// the worker, and the result returns through the studio's Window.Update path.
func (a *App) RefreshUpdates() {
	panel := a.updatesPanel()
	if panel.saving {
		panel.refreshPending = true
		return
	}
	panel.request++
	request := panel.request
	panel.loading = true
	a.run(func() func() {
		status := a.desktop.UpdateStatus()
		return func() {
			if request != panel.request {
				return
			}
			panel.loading = false
			panel.status = status
		}
	})
}

func (a *App) saveUpdatePreferences(checks, downloads bool) {
	panel := a.updatesPanel()
	if panel.loading || panel.saving {
		return
	}
	panel.request++
	request := panel.request
	panel.saving = true
	panel.problem = ""
	a.run(func() func() {
		status, err := a.desktop.SetUpdatePreferences(checks, downloads)
		return func() {
			panel.saving = false
			if request != panel.request {
				return
			}
			panel.status = status
			if err != nil {
				panel.problem = err.Error()
			}
			if panel.refreshPending {
				panel.refreshPending = false
				a.RefreshUpdates()
			}
		}
	})
}

func (a *App) updateSettings(c *ui.Context) {
	panel := a.updatesPanel()
	status := panel.status
	ui.Column(c.Key("updates-section")).FillWidth().Gap(16).Children(func() {
		settingsHeading(c, "软件更新", "自动获取 Picloom 的正式版本")
		version := status.Version
		if version == "" {
			version = "正在读取…"
		}
		ui.Text(c.Key("updates-version"), "当前版本 "+version).FontSize(textSize(c, 14)).FontWeight(500)
		message := "启动后自动检查，此后每天检查一次。"
		if panel.loading {
			message = "正在读取更新设置…"
		} else if !status.Supported {
			message = "开发版或只读安装位置不支持自动更新，请使用正式版并安装到可写目录。"
		}
		ui.Text(c.Key("updates-support"), message).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		lastCheck := "尚未检查"
		if !status.LastCheck.IsZero() {
			lastCheck = status.LastCheck.Local().Format("2006-01-02 15:04")
		}
		ui.Text(c.Key("updates-last-check"), "上次检查："+lastCheck).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		if status.AvailableVersion != "" {
			ui.Text(c.Key("updates-available"), fmt.Sprintf("发现新版本 %s，可查看下载与安装状态。", status.AvailableVersion)).FillWidth().FontSize(textSize(c, 13))
		}
		ui.Divider(c)
		checks, downloads := status.AutomaticChecks, status.AutomaticDownloads
		disabled := panel.loading || panel.saving || !status.Supported
		if ui.Checkbox(c.Key("updates-automatic-checks"), &checks, "自动检查更新").Disabled(disabled).Changed() {
			a.saveUpdatePreferences(checks, downloads)
		}
		if ui.Checkbox(c.Key("updates-automatic-downloads"), &downloads, "自动下载并安装更新").Disabled(disabled || !checks).Changed() {
			a.saveUpdatePreferences(checks, downloads)
		}
		ui.Text(c, "更新包会验证数字签名。后台安装不打断生成任务，新版本在下次启动时生效。").FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		if panel.problem != "" {
			ui.Text(c.Key("updates-error"), panel.problem).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().Danger)
		}
	})
}

func (a *App) updateCheckButton(c *ui.Context) {
	panel := a.updatesPanel()
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		if ui.PrimaryButton(c.Key("check-updates"), "检查更新").Label("检查软件更新").Height(36).Width(136).Disabled(panel.loading || panel.saving || !panel.status.Supported).Clicked() {
			panel.problem = ""
			a.run(func() func() {
				err := a.desktop.CheckForUpdates()
				return func() {
					if err != nil {
						panel.problem = err.Error()
					}
				}
			})
		}
	})
}
