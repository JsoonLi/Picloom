package studio

import (
	"fmt"
	"image2ai/internal/locale"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

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
	if !a.saving && !a.settingsPending() {
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
		a.draftLanguage = a.config.Language
		if a.draftLanguage == "" {
			a.draftLanguage = "system"
		}
	}
	a.settingsTab = "提供商"
	a.settingsOpen = true
	a.showAPIKey = false
	a.autosave.saved = false
	a.problem = ""
	a.notice = ""
}

func (a *App) settings(c *ui.Context, narrow bool) {
	restore := popoverTheme(c)
	defer restore()
	width, height := c.Size()
	contentWidth := min(float32(840), max(float32(240), width-64))
	contentHeight := min(float32(560), max(float32(180), height-200))
	ui.Modal(c.Key("settings-dialog"), &a.settingsOpen, func() {
		ui.Column(c.Key("settings-panel")).Width(contentWidth).MaxWidthPercent(100).Gap(14).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, locale.T("绘图设置")).Grow(1).MinWidth(0).FontSize(textSize(c, 18)).FontWeight(600)
				a.settingsStatus(c)
				if iconButton(c, "close-settings", locale.T("关闭设置"), "close").Size(32, 32).Clicked() {
					a.settingsOpen = false
				}
			})
			ui.Row(c.Key("settings-sections")).FillWidth().Gap(2).Padding(3).Background(selectionTrack(c)).Border(1, c.Theme().Border).Radius(10).Children(func() {
				tabFontSize := float32(13)
				if narrow && locale.Current() == locale.English {
					tabFontSize = 12.5
				}
				for i, tab := range settingsTabs {
					choice := tab
					button := quietButton(c, "settings-tab-"+tab, settingsTabLabels()[i]).Grow(1).MinWidth(0).Height(30).Padding(4, 2).Radius(7).FontSize(textSize(c, tabFontSize))
					if a.settingsTab == tab {
						selectedSurface(c, button).TextColor(c.Theme().Text).FontWeight(600)
					} else {
						button.TextColor(c.Theme().TextMuted)
					}
					if button.Clicked() {
						a.settingsTab = choice
						if choice == "更新" {
							a.RefreshUpdates()
						}
					}
				}
			})
			if a.settingsTab == "提供商" && !narrow {
				a.providerWorkspace(c, contentHeight)
			} else {
				a.settingsPage(c, narrow, contentHeight)
			}
		})
	})
	if ui.AlertDialog(c.Key("delete-provider-dialog"), &a.providerDeleteOpen, locale.T("移除提供商？"), locale.T("这只移除服务配置，已保存的作品仍保留。"), locale.T("取消"), locale.T("移除提供商")) == 1 {
		for i, p := range a.draftProviders {
			if p.ID == a.draftProviderID && a.canRemoveDraftProvider(p.ID) {
				a.draftProviders = append(a.draftProviders[:i], a.draftProviders[i+1:]...)
				delete(a.draftModels, p.ID)
				delete(a.draftModelOptions, p.ID)
				a.invalidateModelDiscovery()
				a.draftProviderID = a.draftProviders[0].ID
				a.queueProviderSave(c, settingsSaveNow)
				break
			}
		}
	}
}

// settingsStatus tells whether the settings are saved, and shows the latest
// error or notice in the same place.
func (a *App) settingsStatus(c *ui.Context) {
	text, color := "", c.Theme().TextMuted
	switch {
	case a.problem != "":
		text, color = a.problem, c.Theme().Danger
	case a.notice != "":
		text = a.notice
	case a.saving || a.settingsPending():
		text = locale.T("正在保存…")
	case a.autosave.saved:
		text = locale.T("已自动保存")
	}
	if text == "" {
		return
	}
	key := "settings-status"
	if a.problem != "" {
		key = "settings-error"
	}
	ui.Text(c.Key(key), text).MaxLines(2).Shrink(1).MinWidth(0).MaxWidth(320).Padding(8, 0, 0, 0).TextAlign(ui.End).FontSize(textSize(c, 12)).TextColor(color)
}

// selectionTrack is the ground under a set of choices. Light mode needs a
// deeper step than the toolbar so a white chosen item stands out on it.
func selectionTrack(c *ui.Context) ui.Color {
	if !c.Preferences().HighContrast && !c.Theme().Dark {
		return ui.Hex("#efeee9")
	}
	return toolbarColor(c)
}

// selectedSurface lifts the chosen item off its track: the paper surface,
// a firmer edge and a soft contact shadow, so it reads in both modes.
func selectedSurface(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	e.Background(t.Surface).Border(1, t.Border.Mix(t.Text, 0.18))
	if c.Preferences().HighContrast {
		return e
	}
	if t.Dark {
		e.Background(t.SurfaceHover).Border(1, t.Border.Mix(t.Text, 0.12))
		return e.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.3))
	}
	return e.Shadow(0, 1, 3, 0, ui.RGBA(40, 36, 28, 0.12))
}

// settingsTabs name the settings pages; they are keys, said through
// settingsTabLabels.
var settingsTabs = []string{"提供商", "网络", "外观", "更新"}

func settingsTabLabels() []string {
	if locale.Current() == locale.English {
		// A short tab name, so all four fit side by side on a phone.
		return []string{"Providers", "Network", "Display", "Updates"}
	}
	return []string{locale.T("提供商"), locale.T("网络"), locale.T("外观"), locale.T("更新")}
}

// settingsPage scrolls one tab's sections. A narrow window also stacks the
// provider list above the chosen provider's details here.
func (a *App) settingsPage(c *ui.Context, narrow bool, height float32) {
	ui.Scroll(c.Key("settings-scroll-"+a.settingsTab)).Height(height).FillWidth().Padding(2, 6, 4, 0).Gap(20).Children(func() {
		switch a.settingsTab {
		case "提供商":
			a.providerList(c, true)
			a.providerDetail(c, true)
		case "网络":
			a.networkSettings(c)
		case "外观":
			a.appearanceSettings(c)
			a.storageSettings(c)
		case "更新":
			a.updateSettings(c)
		}
	})
}

// settingsSection is one titled group: a small heading with optional
// trailing actions over a bordered card whose rows are split by hairlines.
func settingsSection(c *ui.Context, key, title string, actions, body func()) {
	ui.Column(c.Key(key)).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).MinHeight(26).Padding(0, 2).Children(func() {
			ui.Text(c, title).FontSize(textSize(c, 13)).FontWeight(600).Grow(1).MinWidth(0)
			if actions != nil {
				actions()
			}
		})
		ui.Column(c).FillWidth().Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(12).Children(body)
	})
}

// settingsRow keeps a setting's name and explanation on the start side and
// its control on the end side; a narrow panel wraps the control below.
func settingsRow(c *ui.Context, title, description string, control func()) {
	ui.Row(c).FillWidth().Wrap().GapX(16).GapY(8).Padding(12, 14).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Basis(180).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(textSize(c, 14)).FontWeight(500)
			if description != "" {
				settingsNote(c, description)
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(control)
	})
}

func settingsNote(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
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

func (a *App) addDraftProvider() {
	p := domain.Provider{ID: newID(), Name: locale.T("新提供商"), BaseURL: "https://api.example.com/v1", Models: []string{"gpt-image-1"}, Resolutions: []string{"2k", "4k"}, GenerationsPath: "/images/generations", EditsPath: "/images/edits"}
	a.draftProviders = append(a.draftProviders, p)
	a.draftProviderID = p.ID
	a.draftModels[p.ID] = "gpt-image-1"
	a.draftModelOptions[p.ID] = append([]string(nil), p.Models...)
	a.invalidateModelDiscovery()
}

func (a *App) canRemoveDraftProvider(id string) bool {
	if len(a.draftProviders) <= 1 {
		return false
	}
	for _, provider := range a.draftProviders {
		if provider.ID != id && !provider.Disabled {
			return true
		}
	}
	return false
}

// providerWorkspace is the wide provider tab: the providers listed on the
// start side and the chosen provider's details beside them, in one frame.
func (a *App) providerWorkspace(c *ui.Context, height float32) {
	ui.Row(c.Key("provider-workspace")).FillWidth().Height(height).Border(1, c.Theme().Border).Radius(12).Clip().Children(func() {
		ui.Column(c.Key("provider-sidebar")).Width(216).Shrink(0).FillHeight().Background(selectionTrack(c)).Border(1, c.Theme().Border).BorderWidth(0, 1, 0, 0).Children(func() {
			ui.Scroll(c.Key("provider-sidebar-scroll")).Grow(1).MinHeight(0).FillWidth().Padding(10, 8).Children(func() {
				a.providerList(c, false)
			})
			ui.Column(c).FillWidth().Padding(10).Border(1, c.Theme().Border).BorderWidth(1, 0, 0, 0).Children(func() {
				a.addProviderButton(c)
			})
		})
		ui.Scroll(c.Key("settings-scroll-提供商")).Grow(1).MinWidth(0).FillHeight().Padding(0, 0, 20, 0).Children(func() {
			a.providerDetail(c, false)
		})
	})
}

func (a *App) addProviderButton(c *ui.Context) {
	b := ui.Button(c.Key("add-provider"), "").Label(locale.T("新增提供商")).FillWidth().Height(34).Radius(17).Border(1, c.Theme().Border).Background(ui.Transparent)
	if b.Hovered() {
		b.Background(c.Theme().SurfaceHover)
	}
	if b.Pressed() {
		b.Background(c.Theme().SurfacePressed)
	}
	b.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons["plus"]).Size(14, 14)
			ui.Text(c, locale.T("添加提供商")).SingleLine().FontSize(textSize(c, 13)).FontWeight(500)
		})
	})
	if b.Clicked() {
		a.addDraftProvider()
		a.queueProviderSave(c, settingsSaveNow)
	}
}

func providerReady(p domain.Provider) bool {
	return strings.TrimSpace(p.APIKey) != "" && len(p.Models) > 0 && len(p.Resolutions) > 0
}

func providerInitial(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "?"
	}
	r, _ := utf8.DecodeRuneInString(name)
	return strings.ToUpper(string(r))
}

func providerHost(p *domain.Provider) string {
	if u, err := url.Parse(strings.TrimSpace(p.BaseURL)); err == nil && u.Host != "" {
		return u.Host
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return locale.T("尚未填写服务地址")
	}
	return p.BaseURL
}

// providerMark is the provider's monogram on a small bordered tile.
func providerMark(c *ui.Context, name string, size float32) {
	ui.Column(c).Size(size, size).Shrink(0).Center().Radius(size*0.28).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		ui.Text(c, providerInitial(name)).SingleLine().FontSize(textSize(c, size*0.42)).FontWeight(600).TextColor(c.Theme().TextMuted)
	})
}

func (a *App) providerList(c *ui.Context, stacked bool) {
	labels, _, _ := providerLabels(a.draftProviders, a.draftProviderID)
	list := ui.Column(c.Key("provider-list")).FillWidth().Gap(2).Label(locale.T("编辑提供商")).Role(ui.RoleList)
	if stacked {
		list.Padding(6).Background(selectionTrack(c)).Border(1, c.Theme().Border).Radius(12)
	}
	list.Children(func() {
		ui.Text(c, locale.T("提供商")).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted).Padding(2, 8, 6, 8)
		for i := range a.draftProviders {
			p := a.draftProviders[i]
			selected := p.ID == a.draftProviderID
			item := ui.Button(c.Key("provider-item-"+p.ID), "").Label(labels[i]).Role(ui.RoleTab).Selected(selected).FillWidth().Height(48).Padding(0, 8).Radius(10).Border(1, ui.Transparent).Background(ui.Transparent)
			if selected {
				selectedSurface(c, item)
			} else if item.Hovered() {
				item.Background(c.Theme().SurfaceHover)
			}
			if item.Pressed() {
				item.Background(c.Theme().SurfacePressed)
			}
			item.Children(func() {
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					mark := ui.Row(c).Shrink(0).Children(func() { providerMark(c, p.Name, 30) })
					if p.Disabled {
						mark.Opacity(0.5)
					}
					name := ui.Text(c, labels[i]).SingleLine().Grow(1).MinWidth(0).FontSize(textSize(c, 14))
					if selected {
						name.FontWeight(500).TextColor(c.Theme().Text)
					} else {
						name.TextColor(c.Theme().TextMuted)
					}
					count := fmt.Sprintf(locale.T("%d 个模型"), len(p.Models))
					if len(p.Models) == 1 {
						count = locale.T("1 个模型")
					}
					if p.Disabled {
						count = locale.T("已停用")
					}
					ui.Text(c, count).SingleLine().Shrink(0).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
					dot := ui.Box(c).Size(6, 6).Shrink(0).Radius(3)
					if !p.Disabled && providerReady(p) {
						dot.Background(c.Theme().Success)
					} else {
						dot.Background(c.Theme().Border.Mix(c.Theme().Text, 0.2))
					}
				})
			})
			if item.Clicked() && !selected {
				a.draftProviderID = p.ID
				a.invalidateModelDiscovery()
			}
		}
		if stacked {
			ui.Column(c).FillWidth().Padding(6, 2, 2, 2).Children(func() { a.addProviderButton(c) })
		}
	})
}

// providerDetail is the chosen provider: an identity header with its
// actions, then its settings in titled groups.
func (a *App) providerDetail(c *ui.Context, stacked bool) {
	p := a.draftProvider()
	if p == nil {
		return
	}
	header := ui.Row(c.Key("provider-header")).FillWidth().Gap(14).AlignItems(ui.Center)
	if stacked {
		header.Padding(4, 2)
	} else {
		header.Padding(18, 20).Border(1, c.Theme().Border).BorderWidth(0, 0, 1, 0)
	}
	header.Children(func() {
		providerMark(c, p.Name, 44)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c.Key("provider-title"), p.Name).SingleLine().MinWidth(0).FontSize(textSize(c, 17)).FontWeight(600)
				status := locale.T("未配置")
				switch {
				case p.Disabled:
					status = locale.T("已停用")
				case providerReady(*p):
					status = locale.T("可用")
				}
				ui.Row(c).Shrink(0).Height(22).Padding(0, 9).AlignItems(ui.Center).Radius(11).Border(1, c.Theme().Border).Children(func() {
					ui.Text(c, status).SingleLine().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
				})
			})
			ui.Text(c.Key("provider-host"), providerHost(p)).SingleLine().FillWidth().FontSize(textSize(c, 13)).TextColor(c.Theme().TextMuted)
		})
		a.providerSwitch(c, p)
		more := iconButton(c, "provider-more", locale.T("提供商操作"), "more").Size(34, 34).TextColor(c.Theme().TextMuted)
		more.PopoverMenuButton(func(m *ui.Menu) {
			if m.Item(locale.T("移除提供商")).Disabled(!a.canRemoveDraftProvider(p.ID)).Chosen() {
				a.providerDeleteOpen = true
			}
		})
	})
	body := ui.Column(c.Key("provider-body")).FillWidth().Gap(20)
	if !stacked {
		body.Padding(18, 20, 0, 20)
	}
	body.Children(func() { a.providerSections(c, p, stacked) })
}

// providerSwitch turns a provider on or off; the last enabled provider
// stays on so the studio always has a model to use.
func (a *App) providerSwitch(c *ui.Context, p *domain.Provider) {
	enabled := !p.Disabled
	others := 0
	for _, other := range a.draftProviders {
		if other.ID != p.ID && !other.Disabled {
			others++
		}
	}
	label, tip := locale.T("停用提供商"), locale.T("停用后，它的模型不再出现在模型列表里")
	if !enabled {
		label, tip = locale.T("启用提供商"), locale.T("启用后，它的模型重新出现在模型列表里")
	} else if others == 0 {
		tip = locale.T("至少保留一个启用的提供商")
	}
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Children(func() {
		if ui.Switch(c.Key("provider-enabled-"+p.ID), &enabled).Label(label).Tooltip(tip).Disabled(enabled && others == 0).Changed() {
			p.Disabled = !enabled
			a.queueProviderSave(c, settingsSaveNow)
		}
	})
}

func (a *App) providerSections(c *ui.Context, p *domain.Provider, narrow bool) {
	columns := 2
	if narrow {
		columns = 1
	}
	settingsSection(c, "provider-connection", locale.T("连接信息"), nil, func() {
		ui.Column(c).FillWidth().Gap(14).Padding(14).Children(func() {
			ui.Grid(c.Key("provider-fields-" + p.ID)).Columns(columns).Gap(12).Children(func() {
				ui.Field(c, locale.T("名称"), func() {
					if ui.TextInput(c.Key("provider-name"), &p.Name).Height(36).FillWidth().Label(locale.T("提供商名称")).Changed() {
						a.queueProviderSave(c, settingsSaveTyped)
					}
				})
				ui.Field(c, "Base URL", func() {
					if ui.TextInput(c.Key("provider-base-url"), &p.BaseURL).Height(36).FillWidth().Label("Base URL").Placeholder("https://api.example.com/v1").Changed() {
						a.invalidateModelDiscovery()
						a.queueProviderSave(c, settingsSaveTyped)
					}
				})
			})
			ui.Field(c, "API Key", func() {
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					key := ui.TextInput(c.Key("provider-api-key"), &p.APIKey).Height(36).Grow(1).MinWidth(0).Label("API Key")
					if !a.showAPIKey {
						key.Password()
					}
					if key.Changed() {
						a.invalidateModelDiscovery()
						a.queueProviderSave(c, settingsSaveTyped)
					}
					label, icon := locale.T("显示 API Key"), "eye"
					if a.showAPIKey {
						label, icon = locale.T("隐藏 API Key"), "eye-off"
					}
					if iconButton(c, "toggle-api-key", label, icon).Size(36, 36).Border(1, c.Theme().Border).TextColor(c.Theme().TextMuted).Clicked() {
						a.showAPIKey = !a.showAPIKey
					}
				})
			}).Description(locale.T("仅保存在本机，不写入日志"))
		})
	})
	a.modelSettings(c, p)
	r2, r4 := contains(p.Resolutions, "2k"), contains(p.Resolutions, "4k")
	changed := false
	settingsSection(c, "provider-resolutions", locale.T("输出分辨率"), nil, func() {
		settingsRow(c, locale.T("支持分辨率"), locale.T("该服务能生成的尺寸档位"), func() {
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
			a.queueProviderSave(c, settingsSaveNow)
		}
		ui.Divider(c)
		settingsRow(c, locale.T("默认分辨率"), locale.T("切换到此提供商时自动选用"), func() {
			choices := make([]string, 0, len(p.Resolutions))
			for _, resolution := range p.Resolutions {
				choices = append(choices, strings.ToUpper(resolution))
			}
			resolution := strings.ToUpper(providerDefaultResolution(p))
			if ui.Select(c.Key("provider-default-resolution-"+p.ID), &resolution, choices).Label(locale.T("提供商默认分辨率")).Width(96).MinWidth(0).Height(32).Disabled(len(choices) == 0).Changed() {
				p.DefaultResolution = strings.ToLower(resolution)
				a.queueProviderSave(c, settingsSaveNow)
			}
		})
	})
	ui.Collapsible(c.Key("provider-advanced"), locale.T("高级设置 · 接口路径"), &a.settingsAdvanced, func() {
		ui.Column(c).FillWidth().Gap(14).Padding(10, 0, 0, 0).Children(func() {
			settingsNote(c, locale.T("标准接口保持默认路径即可。"))
			ui.Grid(c).Columns(columns).Gap(12).Children(func() {
				ui.Field(c, locale.T("文生图路径"), func() {
					if ui.TextInput(c.Key("generations-path"), &p.GenerationsPath).Height(36).FillWidth().Label(locale.T("文生图路径")).Changed() {
						a.queueProviderSave(c, settingsSaveTyped)
					}
				})
				ui.Field(c, locale.T("参考图编辑路径"), func() {
					if ui.TextInput(c.Key("edits-path"), &p.EditsPath).Height(36).FillWidth().Label(locale.T("参考图编辑路径")).Changed() {
						a.queueProviderSave(c, settingsSaveTyped)
					}
				})
			})
		})
	}).FillWidth().FontSize(textSize(c, 13))
	if len(p.Resolutions) == 0 || len(p.Models) == 0 {
		ui.Text(c, locale.T("至少保留一个模型和一种分辨率。")).FontSize(textSize(c, 12)).TextColor(c.Theme().Danger)
	}
}

func (a *App) modelSettings(c *ui.Context, p *domain.Provider) {
	actions := func() {
		if quietButton(c, "discover-models", locale.T("获取模型")).Height(26).Padding(2, 8).Radius(6).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted).Disabled(a.discoverBusy).Clicked() {
			a.discoverModels(*p)
		}
		if quietButton(c, "refresh-pricing", locale.T("刷新价格")).Height(26).Padding(2, 8).Radius(6).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted).Disabled(a.priceLoading[p.ID]).Clicked() {
			a.refreshPricing(p.ID)
		}
	}
	settingsSection(c, "provider-models", locale.T("启用模型"), actions, func() {
		ui.Column(c).FillWidth().Gap(10).Padding(14).Children(func() {
			ui.Row(c.Key("model-choices-" + p.ID)).FillWidth().Wrap().Gap(8).Children(func() {
				for _, model := range a.draftModelOptions[p.ID] {
					enabled := contains(p.Models, model)
					label := model
					if enabled {
						label = "✓ " + model
					}
					chip := ui.Button(c.Key("provider-model-"+p.ID+"-"+model), label).Label(locale.T("启用模型 ")+model).Role(ui.RoleCheckBox).Checked(enabled).Height(30).Padding(4, 12).MaxWidthPercent(100).Radius(15).FontSize(textSize(c, 13)).Border(1, c.Theme().Border).Background(c.Theme().Surface).TextColor(c.Theme().TextMuted)
					if enabled {
						chip.Background(c.Theme().SurfaceHover).Border(1, c.Theme().Focus).TextColor(c.Theme().Text).FontWeight(500)
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
						a.queueProviderSave(c, settingsSaveNow)
					}
				}
			})
			message := locale.T("可多选，「获取模型」会读取服务端列表。")
			if a.discoverBusy {
				message = locale.T("正在读取模型列表…")
			}
			settingsNote(c, message)
		})
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
				a.showNotice(locale.T("未识别到图片模型，请检查服务地址或稍后重试。"))
				return
			}
			choices := append([]string(nil), p.Models...)
			for _, model := range models {
				if !contains(choices, model) {
					choices = append(choices, model)
				}
			}
			a.draftModelOptions[provider.ID] = choices
			a.showNotice(locale.T("模型列表已更新，点选即可启用。"))
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
	a.runSettingsWrite(func() func() {
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
			a.invalidateBatchPlans()
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
			a.autosave.saved = true
		}
	})
}

func (a *App) networkSettings(c *ui.Context) {
	labels := []string{locale.T("跟随系统"), locale.T("直连"), locale.T("手动代理")}
	label := labels[0]
	switch a.draftProxy.Mode {
	case "direct":
		label = labels[1]
	case "manual":
		label = labels[2]
	}
	settingsSection(c, "network-section", locale.T("网络连接"), nil, func() {
		settingsRow(c, locale.T("连接方式"), locale.T("用于生成请求与图片下载"), func() {
			if ui.Select(c.Key("proxy-mode"), &label, labels).Width(156).MinWidth(0).Height(34).Label(locale.T("选择连接方式")).Changed() {
				switch label {
				case labels[1]:
					a.draftProxy.Mode = "direct"
				case labels[2]:
					a.draftProxy.Mode = "manual"
				default:
					a.draftProxy.Mode = "system"
				}
				a.queueProxySave(c, settingsSaveNow)
			}
		})
		if a.draftProxy.Mode == "manual" {
			ui.Divider(c)
			ui.Column(c.Key("proxy-fields")).FillWidth().Gap(14).Padding(14).Children(func() {
				ui.Field(c, locale.T("代理地址"), func() {
					if ui.TextInput(c.Key("proxy-url"), &a.draftProxy.URL).Height(36).FillWidth().Label(locale.T("代理地址")).Placeholder("http://127.0.0.1:7890").Changed() {
						a.queueProxySave(c, settingsSaveTyped)
					}
				}).Description(locale.T("支持 HTTP、HTTPS、SOCKS4、SOCKS5；不支持内嵌账号密码。"))
				ui.Field(c, locale.T("绕过代理"), func() {
					if ui.TextInput(c.Key("proxy-bypass"), &a.draftProxy.Bypass).Height(36).FillWidth().Label(locale.T("绕过代理")).Changed() {
						a.queueProxySave(c, settingsSaveTyped)
					}
				}).Description(locale.T("使用逗号分隔主机，例如 <local>,localhost,127.0.0.1。"))
			})
		}
	})
}

// saveProxy waits for a manual proxy's address before saving it.
func (a *App) saveProxy() {
	proxy := a.draftProxy
	if proxy.Mode == "manual" && strings.TrimSpace(proxy.URL) == "" {
		return
	}
	a.saving = true
	a.problem = ""
	a.runSettingsWrite(func() func() {
		config, err := a.backend.SaveProxy(proxy)
		return func() {
			a.saving = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.config = config
			a.autosave.saved = true
		}
	})
}

func (a *App) appearanceSettings(c *ui.Context) {
	labels := []string{locale.T("跟随系统"), locale.T("浅色"), locale.T("深色")}
	label := labels[0]
	switch a.draftTheme {
	case "light":
		label = labels[1]
	case "dark":
		label = labels[2]
	}
	settingsSection(c, "appearance-section", locale.T("外观"), nil, func() {
		settingsRow(c, locale.T("主题"), locale.T("浅色、深色，或随系统切换"), func() {
			if ui.Select(c.Key("theme"), &label, labels).Width(156).MinWidth(0).Height(34).Label(locale.T("选择主题")).Changed() {
				switch label {
				case labels[1]:
					a.draftTheme = "light"
				case labels[2]:
					a.draftTheme = "dark"
				default:
					a.draftTheme = "system"
				}
				a.queueThemeSave(c)
			}
		})
		ui.Divider(c)
		languages := []string{"system", locale.Chinese, locale.English}
		names := []string{locale.T("跟随系统"), "简体中文", "English"}
		name := names[0]
		for i, language := range languages {
			if a.draftLanguage == language {
				name = names[i]
			}
		}
		settingsRow(c, locale.T("语言"), locale.T("默认跟随电脑的语言"), func() {
			if ui.Select(c.Key("language"), &name, names).Width(156).MinWidth(0).Height(34).Label(locale.T("选择语言")).Changed() {
				for i := range names {
					if names[i] == name {
						a.draftLanguage = languages[i]
					}
				}
				a.desktop.SetLanguage(a.draftLanguage)
				a.queueLanguageSave(c)
			}
		})
	})
}

func (a *App) saveLanguage() {
	language := a.draftLanguage
	a.saving = true
	a.problem = ""
	a.runSettingsWrite(func() func() {
		config, err := a.backend.SaveLanguage(language)
		return func() {
			a.saving = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.config = config
			a.autosave.saved = true
		}
	})
}

func (a *App) saveTheme() {
	theme := a.draftTheme
	a.saving = true
	a.problem = ""
	a.runSettingsWrite(func() func() {
		config, err := a.backend.SaveTheme(theme)
		return func() {
			a.saving = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.config = config
			a.autosave.saved = true
			a.desktop.SetTheme(theme)
		}
	})
}

// imageDirectory is where new works are saved.
func (a *App) imageDirectory() string {
	if a.config.ImageDirectory != "" {
		return a.config.ImageDirectory
	}
	return filepath.Join(a.desktop.DataDir(), "image-gen")
}

func (a *App) storageSettings(c *ui.Context) {
	settingsSection(c, "storage-section", locale.T("本机存储"), nil, func() {
		ui.Column(c).FillWidth().Gap(8).Padding(12, 14).Children(func() {
			ui.Text(c, locale.T("作品保存位置")).FontSize(textSize(c, 14)).FontWeight(500)
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				directory := a.imageDirectory()
				ui.TextInput(c.Key("image-directory"), &directory).Label(locale.T("作品保存位置")).ReadOnly(true).Height(34).Grow(1).MinWidth(0).Background(toolbarColor(c)).FontSize(textSize(c, 13))
				if ui.Button(c.Key("choose-image-directory"), locale.T("更改…")).Label(locale.T("选择作品保存位置")).Height(34).Padding(6, 14).Disabled(a.saving).Clicked() {
					a.chooseImageDirectory()
				}
			})
			if a.config.ImageDirectory != "" {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					settingsNote(c, locale.T("新作品与参考图保存到这里，之前的作品留在原位置。")).Grow(1).MinWidth(0)
					if quietButton(c, "reset-image-directory", locale.T("恢复默认")).Height(26).Padding(2, 8).Radius(6).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted).Disabled(a.saving).Clicked() {
						a.saveImageDirectory("")
					}
				})
			} else {
				settingsNote(c, locale.T("新作品与参考图保存到这里；更改后，之前的作品留在原位置。"))
			}
		})
		ui.Divider(c)
		ui.Column(c).FillWidth().Gap(8).Padding(12, 14).Children(func() {
			ui.Text(c, locale.T("数据目录")).FontSize(textSize(c, 14)).FontWeight(500)
			directory := a.desktop.DataDir()
			ui.TextInput(c.Key("data-directory"), &directory).Label(locale.T("数据目录")).ReadOnly(true).Height(34).FillWidth().Background(toolbarColor(c)).FontSize(textSize(c, 13))
			settingsNote(c, locale.T("配置文件与缩略图缓存保存在这个目录。"))
		})
	})
}

func (a *App) chooseImageDirectory() {
	current := a.imageDirectory()
	a.run(func() func() {
		directory, ok, err := a.desktop.ChooseDirectory(locale.T("选择作品保存位置"), current)
		return func() {
			if err != nil {
				a.problem = err.Error()
				return
			}
			if ok {
				a.saveImageDirectory(directory)
			}
		}
	})
}

func (a *App) saveImageDirectory(directory string) {
	a.saving = true
	a.problem = ""
	a.runSettingsWrite(func() func() {
		config, err := a.backend.SaveImageDirectory(directory)
		return func() {
			a.saving = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.config = config
			a.autosave.saved = true
		}
	})
}
