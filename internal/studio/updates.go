package studio

import (
	"fmt"
	"image2ai/internal/locale"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

type updatePanel struct {
	status                           domain.UpdateStatus
	loading, saving                  bool
	refreshPending                   bool
	request                          uint64
	problem                          string
	checking, installing, restarting bool
	installRequested, restartFailed  bool
	attemptedVersion                 string
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
	if panel.checking || a.updateApplying() {
		return
	}
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
			a.applyPendingUpdate()
		}
	})
}

func (a *App) saveUpdatePreferences(checks, downloads bool) {
	panel := a.updatesPanel()
	if panel.loading || panel.saving || panel.checking || a.updateApplying() {
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
			a.applyPendingUpdate()
		}
	})
}

func (a *App) updateSettings(c *ui.Context) {
	panel := a.updatesPanel()
	status := panel.status
	ui.Column(c.Key("updates-section")).FillWidth().Gap(20).Children(func() {
		version := status.Version
		if version == "" {
			version = locale.T("正在读取…")
		}
		message := locale.T("启动后自动检查，此后每小时检查一次。")
		if panel.loading {
			message = locale.T("正在读取更新设置…")
		} else if !status.Supported {
			message = locale.T("开发版或只读安装位置不支持自动更新，请使用正式版并安装到可写目录。")
		}
		lastCheck := locale.T("尚未检查")
		if !status.LastCheck.IsZero() {
			lastCheck = status.LastCheck.Local().Format("2006-01-02 15:04")
		}
		settingsSection(c, "updates-version-section", locale.T("软件更新"), nil, func() {
			ui.Row(c).FillWidth().Wrap().GapX(16).GapY(12).Padding(14).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Basis(200).MinWidth(0).Gap(6).Children(func() {
					ui.Text(c.Key("updates-version"), locale.T("当前版本 ")+version).FillWidth().FontSize(textSize(c, 15)).FontWeight(600)
					ui.Text(c.Key("updates-last-check"), locale.T("上次检查：")+lastCheck).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
					ui.Text(c.Key("updates-support"), message).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
					if status.AvailableVersion != "" {
						ui.Text(c.Key("updates-available"), fmt.Sprintf(locale.T("发现新版本 %s，可查看下载与安装状态。"), status.AvailableVersion)).FillWidth().FontSize(textSize(c, 13)).FontWeight(500)
					}
					if panel.checking {
						ui.Text(c, locale.T("正在检查更新…")).FillWidth().FontSize(textSize(c, 12))
					} else if panel.installing {
						ui.Text(c, locale.T("正在下载安装更新，完成后自动重启…")).FillWidth().FontSize(textSize(c, 12))
					} else if status.AvailableVersion != "" && a.updateBusy() {
						ui.Text(c, locale.T("等待生成任务和当前编辑结束后更新。")).FillWidth().FontSize(textSize(c, 12))
					}
				})
				a.updateCheckButton(c)
			})
		})
		checks, downloads := status.AutomaticChecks, status.AutomaticDownloads
		disabled := panel.loading || panel.saving || panel.checking || a.updateApplying() || !status.Supported
		settingsSection(c, "updates-automation-section", locale.T("自动更新"), nil, func() {
			ui.Column(c).FillWidth().Gap(12).Padding(14).Children(func() {
				if ui.Checkbox(c.Key("updates-automatic-checks"), &checks, locale.T("自动检查更新")).Disabled(disabled).Changed() {
					a.saveUpdatePreferences(checks, downloads)
				}
				if ui.Checkbox(c.Key("updates-automatic-downloads"), &downloads, locale.T("自动下载并安装更新")).Disabled(disabled || !checks).Changed() {
					a.saveUpdatePreferences(checks, downloads)
				}
			})
			ui.Divider(c)
			ui.Column(c).FillWidth().Padding(10, 14).Children(func() {
				settingsNote(c, locale.T("更新包会校验数字签名。有生成任务时等待，空闲后自动安装并重启，草稿会自动恢复。"))
			})
		})
		if panel.problem != "" {
			ui.Text(c.Key("updates-error"), panel.problem).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().Danger)
		}
	})
}

func (a *App) updateCheckButton(c *ui.Context) {
	panel := a.updatesPanel()
	ui.Row(c).Shrink(0).Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("check-updates"), locale.T("检查更新")).Label(locale.T("检查软件更新")).Height(34).Padding(6, 16).Disabled(panel.loading || panel.saving || panel.checking || a.updateApplying() || !panel.status.Supported).Clicked() {
			a.checkUpdates(false)
		}
		if panel.status.AvailableVersion != "" && !a.updateApplying() {
			if ui.Button(c.Key("install-update"), locale.T("安装更新")).Height(34).Padding(6, 16).Disabled(panel.loading || panel.saving || panel.checking).Clicked() {
				panel.problem = ""
				panel.installRequested, panel.restartFailed, panel.attemptedVersion = true, false, ""
				a.applyPendingUpdate()
			}
		}
	})
}
