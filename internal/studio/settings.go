package studio

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

func cloneProviders(source []domain.Provider) []domain.Provider {
	target := append([]domain.Provider(nil), source...)
	for i := range target {
		target[i].Models = append([]string(nil), source[i].Models...)
		target[i].Resolutions = append([]string(nil), source[i].Resolutions...)
	}
	return target
}

func (a *App) openSettings() {
	if !a.loaded {
		return
	}
	a.invalidateModelDiscovery()
	a.draftProviders = cloneProviders(a.config.Providers)
	a.draftProviderID = a.providerID
	a.draftModels = make(map[string]string)
	a.draftModelOptions = make(map[string][]string)
	for _, p := range a.draftProviders {
		a.draftModels[p.ID] = strings.Join(p.Models, "\n")
		a.draftModelOptions[p.ID] = append([]string(nil), p.Models...)
	}
	a.draftProxy = a.config.Proxy
	a.draftTheme = a.config.Theme
	a.settingsTab = "提供商"
	a.settingsOpen = true
	a.problem = ""
	a.notice = ""
}

func (a *App) settings(c *ui.Context, narrow bool) {
	restore := popoverTheme(c)
	defer restore()
	width, height := c.Size()
	contentWidth := min(float32(620), max(float32(240), width-64))
	contentHeight := min(float32(480), max(float32(150), height-260))
	ui.Modal(c.Key("settings-dialog"), &a.settingsOpen, func() {
		ui.Column(c.Key("settings-panel")).Width(contentWidth).MaxWidthPercent(100).Gap(12).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					ui.Text(c, "绘图设置").FontSize(textSize(c, 18)).FontWeight(600)
					ui.Text(c, "管理模型服务、网络与外观").FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
				})
				if iconButton(c, "close-settings", "关闭设置", "close").Size(36, 36).Clicked() {
					a.settingsOpen = false
				}
			})
			ui.Row(c.Key("settings-sections")).FillWidth().Gap(4).Padding(4).Background(toolbarColor(c)).Radius(12).Children(func() {
				for _, tab := range []string{"提供商", "网络", "外观"} {
					choice := tab
					button := quietButton(c, "settings-tab-"+tab, tab).Grow(1).MinWidth(0).Height(32).Padding(4, 12).Radius(8)
					if a.settingsTab == tab {
						button.Background(c.Theme().Surface).TextColor(c.Theme().Text).Border(1, c.Theme().Border)
					} else {
						button.TextColor(c.Theme().TextMuted)
					}
					if button.Clicked() {
						a.settingsTab = choice
					}
				}
			})
			ui.Scroll(c.Key("settings-scroll-"+a.settingsTab)).Height(contentHeight).FillWidth().Padding(0, 4, 4, 0).Gap(16).Children(func() {
				switch a.settingsTab {
				case "提供商":
					ui.Column(c.Key("provider-section")).Gap(12).FillWidth().Children(func() {
						ui.Column(c.Key("providers-pane")).Gap(12).FillWidth().Children(func() { a.providerSettings(c, narrow) })
					})
				case "网络":
					ui.Column(c.Key("network-section")).Gap(16).FillWidth().Children(func() {
						settingsHeading(c, "网络连接", "用于生成请求与图片下载")
						a.networkSettings(c)
					})
				case "外观":
					ui.Column(c.Key("appearance-section")).Gap(16).FillWidth().Children(func() {
						settingsHeading(c, "外观", "选择界面主题")
						a.appearanceSettings(c)
					})
					ui.Divider(c)
					ui.Column(c).Gap(12).Children(func() {
						settingsHeading(c, "本机存储", "配置、作品与参考图的保存目录")
						directory := a.desktop.DataDir()
						ui.TextInput(c.Key("data-directory"), &directory).Label("数据目录").ReadOnly(true).Height(36).FillWidth().Background(toolbarColor(c))
					})
				}
			})
			ui.Column(c.Key("settings-footer")).FillWidth().Gap(8).Padding(12, 0, 0, 0).Border(1, c.Theme().Border).BorderWidth(1, 0, 0, 0).Children(func() {
				if a.problem != "" {
					ui.Text(c.Key("settings-error"), a.problem).FontSize(textSize(c, 12)).TextColor(c.Theme().Danger)
				} else if a.notice != "" {
					ui.Text(c.Key("settings-notice"), a.notice).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
				}
				switch a.settingsTab {
				case "网络":
					a.networkSaveButton(c)
				case "外观":
					a.appearanceSaveButton(c)
				default:
					a.providerSaveButton(c)
				}
			})
		})
	})
	if ui.AlertDialog(c.Key("delete-provider-dialog"), &a.providerDeleteOpen, "移除提供商？", "这只移除服务配置，已保存的作品仍保留。保存后生效。", "取消", "移除提供商") == 1 {
		for i, p := range a.draftProviders {
			if p.ID == a.draftProviderID && len(a.draftProviders) > 1 {
				a.draftProviders = append(a.draftProviders[:i], a.draftProviders[i+1:]...)
				delete(a.draftModels, p.ID)
				delete(a.draftModelOptions, p.ID)
				a.invalidateModelDiscovery()
				a.draftProviderID = a.draftProviders[0].ID
				break
			}
		}
	}
}

func settingsHeading(c *ui.Context, title, description string) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, title).FontSize(textSize(c, 16)).FontWeight(500)
		ui.Text(c, description).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	})
}

func (a *App) draftProvider() *domain.Provider {
	for i := range a.draftProviders {
		if a.draftProviders[i].ID == a.draftProviderID {
			return &a.draftProviders[i]
		}
	}
	if len(a.draftProviders) > 0 {
		a.draftProviderID = a.draftProviders[0].ID
		return &a.draftProviders[0]
	}
	return nil
}

func (a *App) providerSettings(c *ui.Context, narrow bool) {
	names, current, ids := providerLabels(a.draftProviders, a.draftProviderID)
	ui.Row(c).FillWidth().Wrap().Gap(8).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(160).Gap(8).Children(func() {
			ui.Text(c, "当前提供商").FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
			if ui.Select(c.Key("settings-provider-select"), &current, names).FillWidth().Height(36).Label("编辑提供商").Changed() {
				a.draftProviderID = ids[current]
				a.invalidateModelDiscovery()
			}
		})
		ui.Row(c).Gap(4).Children(func() {
			ui.Button(c.Key("add-provider"), "新增提供商").Height(36).Padding(8, 16).Disabled(a.saving).OnClick(func() {
				p := domain.Provider{ID: newID(), Name: "新提供商", BaseURL: "https://api.example.com/v1", Models: []string{"gpt-image-1"}, Resolutions: []string{"2k", "4k"}, GenerationsPath: "/images/generations", EditsPath: "/images/edits"}
				a.draftProviders = append(a.draftProviders, p)
				a.draftProviderID = p.ID
				a.draftModels[p.ID] = "gpt-image-1"
				a.draftModelOptions[p.ID] = append([]string(nil), p.Models...)
				a.invalidateModelDiscovery()
			})
			if iconButton(c, "remove-provider", "移除提供商", "trash").TextColor(c.Theme().TextMuted).Disabled(a.saving || len(a.draftProviders) <= 1).Clicked() {
				a.providerDeleteOpen = true
			}
		})
	})
	p := a.draftProvider()
	if p == nil {
		return
	}
	columns := 2
	if narrow {
		columns = 1
	}
	ui.Divider(c)
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		ui.Text(c, "连接信息").FontSize(textSize(c, 14)).FontWeight(500)
		ui.Grid(c.Key("provider-fields-" + p.ID)).Columns(columns).Gap(12).Children(func() {
			ui.Field(c, "名称", func() { ui.TextInput(c.Key("provider-name"), &p.Name).Height(36).FillWidth().Label("提供商名称") })
			ui.Field(c, "Base URL", func() {
				if ui.TextInput(c.Key("provider-base-url"), &p.BaseURL).Height(36).FillWidth().Label("Base URL").Placeholder("https://api.example.com/v1").Changed() {
					a.invalidateModelDiscovery()
				}
			})
		})
		ui.Field(c, "API Key", func() {
			if ui.TextInput(c.Key("provider-api-key"), &p.APIKey).Password().Height(36).FillWidth().Label("API Key").Changed() {
				a.invalidateModelDiscovery()
			}
		}).Description("仅保存在本机，不写入日志")
	})
	ui.Divider(c)
	a.modelSettings(c, p)
	r2, r4 := contains(p.Resolutions, "2k"), contains(p.Resolutions, "4k")
	changed := false
	ui.Row(c.Key("provider-resolutions")).Wrap().FillWidth().Gap(12).Padding(8, 12).AlignItems(ui.Center).Background(toolbarColor(c)).Radius(8).Children(func() {
		ui.Row(c).Grow(1).MinWidth(200).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "支持分辨率").FontSize(textSize(c, 13)).Grow(1)
			if ui.Checkbox(c.Key("supports-2k"), &r2, "2K").Changed() {
				changed = true
			}
			if ui.Checkbox(c.Key("supports-4k"), &r4, "4K").Changed() {
				changed = true
			}
		})
		if changed {
			p.Resolutions = []string{}
			if r2 {
				p.Resolutions = append(p.Resolutions, "2k")
			}
			if r4 {
				p.Resolutions = append(p.Resolutions, "4k")
			}
			if !contains(p.Resolutions, p.DefaultResolution) {
				p.DefaultResolution = first(p.Resolutions)
			}
		}
		ui.Row(c.Key("provider-default-resolution-row")).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "默认分辨率").FontSize(textSize(c, 13))
			choices := make([]string, 0, len(p.Resolutions))
			for _, resolution := range p.Resolutions {
				choices = append(choices, strings.ToUpper(resolution))
			}
			resolution := strings.ToUpper(providerDefaultResolution(p))
			if ui.Select(c.Key("provider-default-resolution-"+p.ID), &resolution, choices).Label("提供商默认分辨率").Width(88).MinWidth(0).Height(32).Disabled(a.saving || len(choices) == 0).Changed() {
				p.DefaultResolution = strings.ToLower(resolution)
			}
		})
	})
	ui.Collapsible(c.Key("provider-advanced"), "高级设置 · 接口路径", &a.settingsAdvanced, func() {
		ui.Column(c).FillWidth().Gap(16).Padding(14, 0, 0, 0).Children(func() {
			ui.Text(c, "服务兼容标准接口时，保留默认路径即可。").FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
			ui.Grid(c).Columns(columns).Gap(16).Children(func() {
				ui.Field(c, "文生图路径", func() {
					ui.TextInput(c.Key("generations-path"), &p.GenerationsPath).Height(36).FillWidth().Label("文生图路径")
				})
				ui.Field(c, "参考图编辑路径", func() {
					ui.TextInput(c.Key("edits-path"), &p.EditsPath).Height(36).FillWidth().Label("参考图编辑路径")
				})
			})
		})
	}).FillWidth()
	valid := len(p.Resolutions) > 0 && len(p.Models) > 0
	if !valid {
		ui.Text(c, "至少保留一个模型和一种分辨率。").TextColor(c.Theme().Danger)
	}
}

func (a *App) modelSettings(c *ui.Context, p *domain.Provider) {
	ui.Column(c.Key("provider-models")).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "启用模型").FontSize(textSize(c, 14)).FontWeight(500).Grow(1)
			if quietButton(c, "discover-models", "获取模型").Height(28).Padding(4, 8).FontSize(textSize(c, 12)).Disabled(a.discoverBusy || a.saving).Clicked() {
				a.discoverModels(*p)
			}
			if quietButton(c, "refresh-pricing", "刷新价格").Height(28).Padding(4, 8).FontSize(textSize(c, 12)).Disabled(a.priceLoading[p.ID] || a.saving).Clicked() {
				a.refreshPricing(p.ID)
			}
		})
		ui.Row(c.Key("model-choices-" + p.ID)).FillWidth().Wrap().Gap(8).Children(func() {
			for _, model := range a.draftModelOptions[p.ID] {
				enabled := contains(p.Models, model)
				label := model
				if enabled {
					label = "✓ " + model
				}
				chip := ui.Button(c.Key("provider-model-"+p.ID+"-"+model), label).Label("启用模型 "+model).Role(ui.RoleCheckBox).Checked(enabled).Height(32).Padding(4, 12).MaxWidthPercent(100).Radius(16).FontSize(textSize(c, 13)).Border(1, c.Theme().Border).Background(c.Theme().Surface).TextColor(c.Theme().TextMuted).Disabled(a.saving)
				if enabled {
					chip.Background(c.Theme().SurfaceHover).Border(1, c.Theme().Focus).TextColor(c.Theme().Text)
				}
				if chip.Hovered() {
					chip.Background(c.Theme().SurfaceHover)
				}
				if chip.Pressed() {
					chip.Background(c.Theme().SurfacePressed)
				}
				if chip.Clicked() {
					models := make([]string, 0, len(p.Models)+1)
					for _, id := range p.Models {
						if id != model {
							models = append(models, id)
						}
					}
					if !enabled {
						models = append(models, model)
					}
					p.Models = models
					a.draftModels[p.ID] = strings.Join(models, "\n")
				}
			}
		})
		message := "可多选；点击「获取模型」读取服务支持的模型。"
		if a.discoverBusy {
			message = "正在读取模型列表…"
		}
		ui.Text(c, message).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	})
}

func (a *App) providerSaveButton(c *ui.Context) {
	p := a.draftProvider()
	valid := p != nil && len(p.Resolutions) > 0 && len(splitModels(a.draftModels[p.ID])) > 0
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		if ui.PrimaryButton(c.Key("save-providers"), "保存提供商").Height(36).Width(136).Disabled(a.saving || !valid).Clicked() {
			a.saveProviders()
		}
	})
}

func splitModels(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' || r == '，' })
	result := []string{}
	for _, field := range fields {
		value := strings.TrimSpace(field)
		if value != "" && !contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func (a *App) invalidateModelDiscovery() {
	a.modelRequest++
	a.discoverBusy = false
}

func (a *App) discoverModels(provider domain.Provider) {
	a.modelRequest++
	request := a.modelRequest
	a.discoverBusy = true
	a.problem = ""
	a.run(func() func() {
		models, err := a.backend.DiscoverModels(a.ctx, provider)
		return func() {
			if request != a.modelRequest {
				return
			}
			a.discoverBusy = false
			if !a.settingsOpen || a.draftProviderID != provider.ID {
				return
			}
			p := a.draftProvider()
			if p == nil || p.ID != provider.ID || p.BaseURL != provider.BaseURL || p.APIKey != provider.APIKey {
				return
			}
			if err != nil {
				a.problem = err.Error()
				return
			}
			if len(models) == 0 {
				a.showNotice("未识别到图片模型，请检查服务地址或稍后重试。")
				return
			}
			choices := append([]string(nil), p.Models...)
			for _, model := range models {
				if !contains(choices, model) {
					choices = append(choices, model)
				}
			}
			a.draftModelOptions[provider.ID] = choices
			a.showNotice("模型列表已更新，勾选后保存提供商。")
		}
	})
}

func (a *App) saveProviders() {
	providers := cloneProviders(a.draftProviders)
	for i := range providers {
		providers[i].Models = splitModels(a.draftModels[providers[i].ID])
	}
	a.saving = true
	a.problem = ""
	a.run(func() func() {
		config, err := a.backend.SaveProviders(providers)
		return func() {
			a.saving = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			previousDefault := providerDefaultResolution(a.provider())
			previousResolution := a.resolution
			a.config = config
			if p := a.provider(); p != nil && providerDefaultResolution(p) != previousDefault {
				a.resolution = providerDefaultResolution(p)
			}
			a.reconcileSelection()
			if a.resolution != previousResolution {
				a.savePreferences()
			}
			a.pricingEpoch++
			a.pricing = make(map[string][]domain.ModelPricing)
			a.priceLoading = make(map[string]bool)
			a.loadPricing()
			a.showNotice("提供商已保存。")
		}
	})
}

func (a *App) networkSettings(c *ui.Context) {
	labels := []string{"跟随系统", "直连", "手动代理"}
	label := labels[0]
	switch a.draftProxy.Mode {
	case "direct":
		label = labels[1]
	case "manual":
		label = labels[2]
	}
	ui.Field(c, "连接方式", func() {
		if ui.Select(c.Key("proxy-mode"), &label, labels).Width(280).MaxWidthPercent(100).Height(36).Label("连接方式").Changed() {
			switch label {
			case labels[1]:
				a.draftProxy.Mode = "direct"
			case labels[2]:
				a.draftProxy.Mode = "manual"
			default:
				a.draftProxy.Mode = "system"
			}
		}
	})
	if a.draftProxy.Mode == "manual" {
		ui.Field(c, "代理地址", func() {
			ui.TextInput(c.Key("proxy-url"), &a.draftProxy.URL).Height(36).FillWidth().Label("代理地址").Placeholder("http://127.0.0.1:7890")
		}).Description("支持 HTTP、HTTPS、SOCKS4、SOCKS5；不支持内嵌账号密码。")
		ui.Field(c, "绕过代理", func() {
			ui.TextInput(c.Key("proxy-bypass"), &a.draftProxy.Bypass).Height(36).FillWidth().Label("绕过代理")
		}).Description("使用逗号分隔主机，例如 <local>,localhost,127.0.0.1。")
	}
}

func (a *App) networkSaveButton(c *ui.Context) {
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		if ui.PrimaryButton(c.Key("save-proxy"), "保存网络设置").Width(136).Height(36).Disabled(a.saving).Clicked() {
			proxy := a.draftProxy
			a.saving = true
			a.problem = ""
			a.run(func() func() {
				config, err := a.backend.SaveProxy(proxy)
				return func() {
					a.saving = false
					if err != nil {
						a.problem = err.Error()
						return
					}
					a.config = config
					a.showNotice("网络设置已保存，新请求将使用此配置。")
				}
			})
		}
	})
}

func (a *App) appearanceSettings(c *ui.Context) {
	labels := []string{"跟随系统", "浅色", "深色"}
	label := labels[0]
	switch a.draftTheme {
	case "light":
		label = labels[1]
	case "dark":
		label = labels[2]
	}
	ui.Field(c, "主题", func() {
		if ui.Select(c.Key("theme"), &label, labels).Width(280).MaxWidthPercent(100).Height(36).Label("主题").Changed() {
			switch label {
			case labels[1]:
				a.draftTheme = "light"
			case labels[2]:
				a.draftTheme = "dark"
			default:
				a.draftTheme = "system"
			}
		}
	})
}

func (a *App) appearanceSaveButton(c *ui.Context) {
	ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
		if ui.PrimaryButton(c.Key("save-theme"), "保存外观设置").Width(136).Height(36).Disabled(a.saving).Clicked() {
			theme := a.draftTheme
			a.saving = true
			a.problem = ""
			a.run(func() func() {
				config, err := a.backend.SaveTheme(theme)
				return func() {
					a.saving = false
					if err != nil {
						a.problem = err.Error()
						return
					}
					a.config = config
					a.desktop.SetTheme(theme)
				}
			})
		}
	})
}
