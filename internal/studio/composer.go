package studio

import (
	"fmt"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

const (
	composerPromptHeight   = 88
	composerPromptFontSize = 14
	composerControlHeight  = 32
	composerToolbarPadding = 8
)

func (a *App) composer(c *ui.Context, narrow bool) {
	card := glassPane(c, "composer").FillWidth().Radius(12).Clip()
	if !c.Preferences().HighContrast {
		opacity := float32(0.05)
		if c.Theme().Dark {
			opacity = 0.16
		}
		card.Shadow(0, 2, 8, -4, ui.RGBA(0, 0, 0, opacity))
	}
	if card.FocusWithin() && !a.settingsOpen && !a.previewOpen && !a.deleteOpen {
		focus := c.Theme().Focus
		if !c.Preferences().HighContrast {
			focus = c.Theme().Border.Mix(c.Theme().Text, 0.12)
		}
		card.Border(1, focus)
	}
	if paths := card.DroppedFiles(); len(paths) > 0 {
		copied := append([]string(nil), paths...)
		a.importImages(func(remaining int) ([]domain.Reference, error) { return a.desktop.ImportFiles(copied, remaining) })
	}
	card.Children(func() {
		_, windowHeight := c.Size()
		promptMaxHeight := max(float32(composerPromptHeight), min(float32(320), windowHeight*0.35))
		placeholderColor := c.Theme().TextMuted
		if !c.Preferences().HighContrast {
			placeholderColor = placeholderColor.Mix(c.Theme().Surface, 0.15)
		}
		prompt := ui.TextArea(c.Key("prompt"), &a.prompt).Label("提示词").Placeholder("描述画面主体、光线、构图与风格").PlaceholderColor(placeholderColor).Lines(1, 12).MinHeight(composerPromptHeight).MaxHeight(promptMaxHeight).FillWidth().Padding(16).FontSize(textSize(c, composerPromptFontSize)).FontWeight(400).Background(ui.Transparent).Border(0, ui.Transparent).Radius(0).OnPaste(func(text string) bool {
			if text == "" {
				a.importImages(a.desktop.PasteImages)
				return true
			}
			return false
		})
		prompt.OnChange(func() {
			if prompt.Composing() {
				return
			}
			anchor, caret := prompt.TextAnchorCaret()
			cleaned, selection := compactPrompt(a.prompt, [2]int{anchor, caret})
			if cleaned != a.prompt {
				a.prompt = cleaned
				prompt.SetTextSelection(selection[0], selection[1])
			}
		})
		if len(a.references) > 0 {
			ui.Row(c.Key("reference-list")).Wrap().Gap(12).Padding(0, 16, 12, 16).Children(func() {
				for _, reference := range a.references {
					ref := reference
					ui.Box(c.Key("reference-"+ref.ID)).Size(64, 64).Children(func() {
						if thumb := a.referenceThumbs[ref.ID]; thumb != nil {
							ui.Image(c.Key("reference-image-"+ref.ID), thumb).Fill().Fit(ui.Contain).Radius(10).Border(1, c.Theme().Border).Label("标注编辑参考图 " + ref.Name).Focusable().OnClick(func() { a.openReference(ref) })
						}
						if a.draftMarkup != "" {
							ui.Text(c, "含标注").Absolute().Bottom(2).Left(4).Right(4).TextAlign(ui.Center).FontSize(textSize(c, 10)).Background(c.Theme().Surface).Radius(4)
						}
						iconButton(c, "remove-reference-"+ref.ID, "移除参考图 "+ref.Name, "close").Absolute().Top(-7).Right(-7).Size(24, 24).Background(c.Theme().SurfaceHover).Radius(12).OnClick(func() { a.removeReference(ref.ID) })
					})
				}
			})
		}
		a.generationToolbar(c, narrow)
	})
	ui.Row(c.Key("generation-summary")).FillWidth().Wrap().Gap(8).Padding(12, 0, 0, 0).Children(func() {
		ui.Row(c).Wrap().Gap(8).Children(func() {
			size := strings.ReplaceAll(a.requestSize(), "x", " × ") + " px"
			if a.requestSize() == "auto" {
				size = "自动尺寸"
			}
			price := a.priceLabel()
			if a.outputFormat == "gif" {
				size += " / 帧"
				price = "GIF 费用以渠道账单为准"
			}
			ui.Text(c.Key("generation-size"), size).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
			if a.outputFormat == "gif" {
				ui.Text(c.Key("gif-request-duration"), fmt.Sprintf("请求约 %.2g 秒", float64(a.gifFrameCount*a.gifFrameDurationMS)/1000)).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
			}
			ui.Text(c.Key("generation-price"), price).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		})
		ui.Spacer(c)
		ui.Row(c.Key("generation-shortcut")).Gap(4).AlignItems(ui.Center).Children(func() {
			modifier := "Ctrl"
			if runtime.GOOS == "darwin" {
				modifier = "⌘"
			}
			for _, key := range []string{modifier, "Enter"} {
				ui.Text(c, key).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted).Padding(2, 4).Border(1, c.Theme().Border).Radius(4)
			}
		})
	})
	if utf8.RuneCountInString(a.prompt) > 20000 {
		ui.Text(c, "提示词不能超过 20000 字。").TextColor(c.Theme().Danger)
	}
	if problem := a.gifOptionsProblem(); problem != "" {
		ui.Text(c.Key("gif-options-problem"), problem).TextColor(c.Theme().Danger)
	}
}

func (a *App) generationToolbar(c *ui.Context, narrow bool) {
	ui.Row(c.Key("generation-actions")).FillWidth().Wrap().Gap(8).Padding(composerToolbarPadding).Background(c.Theme().Surface).Children(func() {
		ui.Row(c.Key("reference-actions")).Gap(4).Children(func() {
			refs := ui.Button(c.Key("open-images"), "").Label("添加参考图").Height(composerControlHeight).Width(128).Padding(4, 12).Radius(8).Disabled(a.importing || len(a.references) >= 4 || a.draftMarkup != "")
			refs.Children(func() {
				ui.Row(c).Center().Gap(8).Children(func() {
					ui.Icon(c, icons["image"]).Size(16, 16)
					limit := 4
					if a.draftMarkup != "" {
						limit = 1
					}
					ui.Text(c, fmt.Sprintf("参考图 %d/%d", len(a.references), limit)).FontSize(textSize(c, 14)).SingleLine()
				})
			})
			if refs.Clicked() {
				a.importImages(a.desktop.OpenImages)
			}
		})
		labels, current, choices := a.modelChoices()
		modelSelect := ui.Select(c.Key("model"), &current, labels).Label("模型").Width(256).MaxWidthPercent(100).Height(composerControlHeight).Padding(4, 12).Radius(8)
		if narrow {
			modelSelect.FillWidth()
		}
		if modelSelect.Changed() {
			choice := choices[current]
			a.providerID, a.model = choice.providerID, choice.model
			a.draftSize = ""
			a.resolution = ""
			a.reconcileSelection()
			a.savePreferences()
			a.loadPricing()
		}
		ui.Row(c.Key("format-actions")).Gap(8).Children(func() {
			if a.outputFormat == "gif" {
				a.gifSizeControl(c, narrow)
				return
			}
			ratioWidth, resolutionWidth := float32(96), float32(88)
			if narrow {
				ratioWidth, resolutionWidth = 80, 72
			}
			ratio, ratioOptions := a.ratio, ratios
			if a.draftSize != "" {
				ratio, ratioOptions = "沿用", append([]string{"沿用"}, ratios...)
			}
			if ui.Select(c.Key("ratio"), &ratio, ratioOptions).Label("画面比例").Width(ratioWidth).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() && ratio != "沿用" {
				a.ratio, a.draftSize = ratio, ""
				a.savePreferences()
			}
			resolutions := []string{"2K", "4K"}
			if p := a.provider(); p != nil {
				resolutions = nil
				for _, resolution := range p.Resolutions {
					resolutions = append(resolutions, strings.ToUpper(resolution))
				}
			}
			resolution := strings.ToUpper(a.resolution)
			if a.draftSize != "" {
				resolution, resolutions = "沿用", append([]string{"沿用"}, resolutions...)
			}
			if ui.Select(c.Key("resolution"), &resolution, resolutions).Label("分辨率").Width(resolutionWidth).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() && resolution != "沿用" {
				a.resolution = strings.ToLower(resolution)
				a.draftSize = ""
				a.savePreferences()
			}
		})
		format := strings.ToUpper(a.outputFormat)
		if format == "WEBP" {
			format = "WebP"
		}
		if format == "" {
			format = "默认格式"
		}
		formats := []string{"默认格式", "PNG", "JPEG", "WebP", "GIF", "SVG"}
		if a.draftMarkup != "" {
			formats = formats[:4]
		}
		if ui.Select(c.Key("output-format"), &format, formats).Label("输出格式").Width(104).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() {
			a.outputFormat = strings.ToLower(format)
			if format == "默认格式" {
				a.outputFormat = ""
			}
			a.savePreferences()
		}
		if a.outputFormat == "gif" {
			a.gifTimingControls(c)
		}
		ui.Row(c.Key("generate-slot")).Grow(1).MinWidth(116).Justify(ui.End).Children(func() {
			button := ui.PrimaryButton(c.Key("generate"), "开始生成").Label("开始生成").Height(composerControlHeight).Width(116).Padding(4, 12).Radius(8).Disabled(!a.canGenerate())
			if button.Clicked() {
				a.generate()
			}
		})
	})
}

type modelChoice struct{ providerID, model string }

func (a *App) modelChoices() ([]string, string, map[string]modelChoice) {
	labels := []string{}
	choices := map[string]modelChoice{}
	current := ""
	counts := map[string]int{}
	for _, p := range a.config.Providers {
		for _, m := range p.Models {
			counts[p.Name+" / "+m]++
		}
	}
	for _, p := range a.config.Providers {
		for _, m := range p.Models {
			label := p.Name + " / " + m
			if counts[label] > 1 {
				label += " (" + p.ID + ")"
			}
			labels = append(labels, label)
			choices[label] = modelChoice{p.ID, m}
			if a.providerID == p.ID && a.model == m {
				current = label
			}
		}
	}
	return labels, current, choices
}
func (a *App) canGenerate() bool {
	return strings.TrimSpace(a.prompt) != "" && utf8.RuneCountInString(a.prompt) <= 20000 && a.provider() != nil && a.model != "" && !a.importing && a.gifOptionsProblem() == "" && (a.draftMarkup == "" || (a.outputFormat != "svg" && a.outputFormat != "gif"))
}
func (a *App) clearInput() {
	a.referenceEpoch++
	a.importing = false
	a.prompt = ""
	a.draftSize, a.draftMarkup = "", ""
	a.references = nil
	a.referenceThumbs = map[string]*ui.Bitmap{}
}

func (a *App) importImages(read func(int) ([]domain.Reference, error)) {
	if a.draftMarkup != "" {
		a.problem = "标注编辑仅支持一张参考图，请先移除当前图片后再添加。"
		return
	}
	if a.importing {
		return
	}
	if len(a.references) >= 4 {
		a.problem = "最多添加 4 张参考图，请先移除一张。"
		return
	}
	remaining := 4 - len(a.references)
	a.referenceEpoch++
	epoch := a.referenceEpoch
	a.importing = true
	a.run(func() func() {
		refs, err := read(remaining)
		truncated := len(refs) > remaining
		if len(refs) > remaining {
			refs = refs[:remaining]
		}
		thumbs := make(map[string]*ui.Bitmap)
		if err == nil {
			for i := range refs {
				if refs[i].ID == "" {
					refs[i].ID = newID()
				}
				data, e := referenceBytes(refs[i])
				if e != nil {
					err = e
					break
				}
				thumbs[refs[i].ID], e = thumbnail(data, referenceThumbSize, referenceThumbSize)
				if e != nil {
					err = fmt.Errorf("参考图无法解码，请重新选择有效图片。")
					break
				}
			}
		}
		return func() {
			if epoch != a.referenceEpoch {
				return
			}
			a.importing = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			if len(refs) == 0 {
				return
			}
			accepted := 0
			for _, ref := range refs {
				if len(a.references) >= 4 {
					break
				}
				a.references = append(a.references, ref)
				a.referenceThumbs[ref.ID] = thumbs[ref.ID]
				accepted++
			}
			if truncated || accepted < len(refs) {
				a.problem = "最多添加 4 张参考图，超出的图片未添加。"
			}
		}
	})
}

func (a *App) removeReference(id string) {
	for i, ref := range a.references {
		if ref.ID == id {
			a.references = append(a.references[:i], a.references[i+1:]...)
			if len(a.references) == 0 {
				if a.draftMarkup != "" && a.draftSize == "auto" {
					a.draftSize = ""
				}
				a.draftMarkup = ""
			}
			delete(a.referenceThumbs, id)
			return
		}
	}
}

func (a *App) generate() {
	args := domain.GenerateArgs{JobID: newID(), Prompt: strings.TrimRight(a.prompt, "\n"), ProviderID: a.providerID, Model: a.model, Size: a.requestSize(), OutputFormat: a.outputFormat, Markup: a.draftMarkup, ReferenceImages: []string{}}
	if a.outputFormat == "gif" {
		args.FrameCount = a.gifFrameCount
		args.FrameDurationMS = a.gifFrameDurationMS
	}
	for _, ref := range a.references {
		args.ReferenceImages = append(args.ReferenceImages, ref.DataURL)
	}
	name := ""
	if p := a.provider(); p != nil {
		name = p.Name
	}
	a.startJob(args, name)
	a.clearInput()
}

func (a *App) startJob(args domain.GenerateArgs, name string) {
	task := &job{Args: args, ProviderName: name, Progress: domain.Progress{JobID: args.JobID, Status: "generating", Stage: "准备请求"}}
	a.jobs = append([]*job{task}, a.jobs...)
	a.problem = ""
	a.notice = ""
	a.run(func() func() {
		_, err := a.backend.Generate(a.ctx, args, func(p domain.Progress) { a.post(func() { task.Progress = p }) })
		return func() {
			if err != nil {
				task.Progress.Status = "error"
				task.Progress.Stage = "生成失败"
				task.Progress.Error = err.Error()
				return
			}
			for i, j := range a.jobs {
				if j == task {
					a.jobs = append(a.jobs[:i], a.jobs[i+1:]...)
					break
				}
			}
			a.showNotice("图片已生成并保存到本机作品库。")
			a.refreshHistory()
		}
	})
}

func (a *App) savePreferences() {
	prefs := domain.GenerationPreferences{ProviderID: a.providerID, Model: a.model, Ratio: a.ratio, Resolution: a.resolution, OutputFormat: a.outputFormat}
	prefs.GIFSize = a.gifSize
	prefs.GIFFrameCount = a.gifFrameCount
	prefs.GIFFrameDurationMS = a.gifFrameDurationMS
	a.pendingPreferences = &prefs
	if !a.preferencesSaving {
		a.flushPreferences()
	}
}

// Only one preference write may reach the backend at a time. UI events arriving
// during a write replace its queued successor with their latest immutable snapshot.
func (a *App) flushPreferences() {
	if a.pendingPreferences == nil || a.ctx.Err() != nil {
		return
	}
	prefs := *a.pendingPreferences
	a.pendingPreferences = nil
	a.preferencesSaving = true
	a.run(func() func() {
		_, err := a.backend.SavePreferences(prefs)
		return func() {
			a.preferencesSaving = false
			if err != nil {
				a.problem = err.Error()
			}
			a.flushPreferences()
		}
	})
}

func (a *App) loadPricing() {
	id := a.providerID
	if id == "" || a.priceLoading[id] {
		return
	}
	if _, ok := a.pricing[id]; ok {
		return
	}
	a.requestPricing(id, false)
}

func (a *App) refreshPricing(id string) {
	if id == "" || a.priceLoading[id] {
		return
	}
	delete(a.pricing, id)
	a.requestPricing(id, true)
}

// All price discovery paths belong to the saved configuration epoch. A result
// from before a provider save must not clear a newer request's loading state.
func (a *App) requestPricing(id string, notify bool) {
	epoch := a.pricingEpoch
	a.priceLoading[id] = true
	a.run(func() func() {
		rows, err := a.backend.DiscoverPricing(a.ctx, id)
		return func() {
			if epoch != a.pricingEpoch {
				return
			}
			a.priceLoading[id] = false
			if err != nil {
				a.pricing[id] = nil
			} else {
				a.pricing[id] = rows
			}
			if notify {
				if err != nil {
					a.problem = "暂时无法读取价格，请先保存服务配置后重试。"
				} else {
					a.showNotice("价格已更新。")
				}
			}
		}
	})
}

func (a *App) priceLabel() string {
	if a.priceLoading[a.providerID] {
		return "正在查询价格…"
	}
	for _, price := range a.pricing[a.providerID] {
		if price.Model == a.model {
			if value, ok := price.Prices[a.resolution]; ok {
				return fmt.Sprintf("%s %g / 张", price.Currency, value)
			}
		}
	}
	return "价格暂不可用"
}
