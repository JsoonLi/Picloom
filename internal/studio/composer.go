package studio

import (
	"errors"
	"fmt"
	"image2ai/internal/locale"
	"math"
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
		prompt := ui.TextArea(c.Key("prompt"), &a.prompt).Label(locale.T("提示词")).Placeholder(locale.T("描述画面主体、光线、构图与风格")).PlaceholderColor(placeholderColor).Lines(1, 12).MinHeight(composerPromptHeight).MaxHeight(promptMaxHeight).FillWidth().Padding(16).FontSize(textSize(c, composerPromptFontSize)).FontWeight(400).Background(ui.Transparent).Border(0, ui.Transparent).Radius(0).OnPaste(func(text string) bool {
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
							ui.Image(c.Key("reference-image-"+ref.ID), thumb).Fill().Fit(ui.Contain).Radius(10).Border(1, c.Theme().Border).Label(locale.T("标注编辑参考图 ") + ref.Name).Focusable().OnClick(func() { a.openReference(ref) })
						}
						if a.draftMarkup != "" {
							ui.Text(c, locale.T("含标注")).Absolute().Bottom(2).Left(4).Right(4).TextAlign(ui.Center).FontSize(textSize(c, 10)).Background(c.Theme().Surface).Radius(4)
						}
						iconButton(c, "remove-reference-"+ref.ID, locale.T("移除参考图 ")+ref.Name, "close").Absolute().Top(-7).Right(-7).Size(24, 24).Background(c.Theme().SurfaceHover).Radius(12).OnClick(func() { a.removeReference(ref.ID) })
					})
				}
			})
		}
		a.generationToolbar(c, narrow)
	})
	ui.Row(c.Key("generation-summary")).FillWidth().Wrap().Gap(8).Padding(12, 0, 0, 0).Children(func() {
		price := a.priceLabel()
		if a.outputFormat == "gif" {
			price = locale.T("GIF 费用以渠道账单为准")
		}
		if price != "" {
			ui.Text(c.Key("generation-price"), price).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		}
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
		ui.Text(c, locale.T("提示词不能超过 20000 字。")).TextColor(c.Theme().Danger)
	}
}

func (a *App) generationToolbar(c *ui.Context, narrow bool) {
	ui.Row(c.Key("generation-actions")).FillWidth().Wrap().Gap(8).Padding(composerToolbarPadding).Background(c.Theme().Surface).Children(func() {
		ui.Row(c.Key("reference-actions")).Gap(4).Children(func() {
			refs := ui.Button(c.Key("open-images"), "").Label(locale.T("添加参考图")).Height(composerControlHeight).Width(128).Padding(4, 12).Radius(8).Disabled(a.importing || len(a.references) >= domain.MaxReferenceImages || a.draftMarkup != "")
			refs.Children(func() {
				ui.Row(c).Center().Gap(8).Children(func() {
					ui.Icon(c, icons["image"]).Size(16, 16)
					limit := domain.MaxReferenceImages
					if a.draftMarkup != "" {
						limit = 1
					}
					ui.Text(c, fmt.Sprintf(locale.T("参考图 %d/%d"), len(a.references), limit)).FontSize(textSize(c, 14)).SingleLine()
				})
			})
			if refs.Clicked() {
				a.importImages(a.desktop.OpenImages)
			}
		})
		labels, current, choices := a.modelChoices()
		modelSelect := ui.Select(c.Key("model"), &current, labels).Label(locale.T("模型")).Width(256).MaxWidthPercent(100).Height(composerControlHeight).Padding(4, 12).Radius(8)
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
			a.loadPricing(true)
		}
		ui.Row(c.Key("format-actions")).Gap(8).Children(func() {
			if a.outputFormat == "gif" {
				a.gifSizeControl(c, narrow)
				return
			}
			ratioWidth, resolutionWidth := float32(128), float32(88)
			if locale.Current() == locale.English {
				ratioWidth = 184
			}
			if narrow {
				resolutionWidth = 72
			}
			ratio, ratioOptions := ratioLabel(a.ratio), ratioLabels()
			if a.draftSize != "" {
				ratio, ratioOptions = locale.T("沿用"), append([]string{locale.T("沿用")}, ratioOptions...)
			}
			if ui.Select(c.Key("ratio"), &ratio, ratioOptions).Label(locale.T("画面比例")).Width(ratioWidth).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() && ratio != locale.T("沿用") {
				a.ratio, _, _ = strings.Cut(ratio, " ")
				a.draftSize = ""
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
				resolution, resolutions = locale.T("沿用"), append([]string{locale.T("沿用")}, resolutions...)
			}
			if ui.Select(c.Key("resolution"), &resolution, resolutions).Label(locale.T("分辨率")).Width(resolutionWidth).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() && resolution != locale.T("沿用") {
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
			format = locale.T("默认格式")
		}
		formats := []string{locale.T("默认格式"), "PNG", "JPEG", "WebP", "GIF", "SVG"}
		if a.draftMarkup != "" {
			formats = formats[:4]
		}
		if ui.Select(c.Key("output-format"), &format, formats).Label(locale.T("输出格式")).Width(104).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() {
			a.outputFormat = strings.ToLower(format)
			if format == locale.T("默认格式") {
				a.outputFormat = ""
			}
			a.savePreferences()
		}
		if a.outputFormat == "gif" {
			a.gifTimingControls(c)
		}
		ui.Row(c.Key("generate-slot")).Grow(1).MinWidth(116).Justify(ui.End).Children(func() {
			button := ui.PrimaryButton(c.Key("generate"), locale.T("开始生成")).Label(locale.T("开始生成")).Height(composerControlHeight).Width(116).Padding(4, 12).Radius(8).Disabled(!a.canGenerate())
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
		if p.Disabled {
			continue
		}
		for _, m := range p.Models {
			counts[p.Name+" / "+m]++
		}
	}
	for _, p := range a.config.Providers {
		if p.Disabled {
			continue
		}
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
	return !a.updateApplying() && strings.TrimSpace(a.prompt) != "" && utf8.RuneCountInString(a.prompt) <= 20000 && a.provider() != nil && a.model != "" && !a.importing && a.gifOptionsProblem() == "" && (a.draftMarkup == "" || (a.outputFormat != "svg" && a.outputFormat != "gif"))
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
	if a.updateRestarting() {
		return
	}
	if a.draftMarkup != "" {
		a.problem = locale.T("标注编辑仅支持一张参考图，请先移除当前图片后再添加。")
		return
	}
	if a.importing {
		return
	}
	if len(a.references) >= domain.MaxReferenceImages {
		a.problem = fmt.Sprintf(locale.T("最多添加 %d 张参考图，请先移除一张。"), domain.MaxReferenceImages)
		return
	}
	remaining := domain.MaxReferenceImages - len(a.references)
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
					err = errors.New(locale.T("参考图无法解码，请重新选择有效图片。"))
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
				if len(a.references) >= domain.MaxReferenceImages {
					break
				}
				a.references = append(a.references, ref)
				a.referenceThumbs[ref.ID] = thumbs[ref.ID]
				accepted++
			}
			if truncated || accepted < len(refs) {
				a.problem = fmt.Sprintf(locale.T("最多添加 %d 张参考图，超出的图片未添加。"), domain.MaxReferenceImages)
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
	if a.updateApplying() {
		return
	}
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
	if a.updateApplying() {
		return
	}
	task := &job{Args: args, ProviderName: name, running: true, Progress: domain.Progress{JobID: args.JobID, Status: "generating", Stage: locale.T("准备请求")}}
	a.jobs = append([]*job{task}, a.jobs...)
	a.problem = ""
	a.notice = ""
	a.run(func() func() {
		_, err := a.backend.Generate(a.ctx, args, func(p domain.Progress) { a.post(func() { task.Progress = p }) })
		return func() {
			task.running = false
			defer a.applyPendingUpdate()
			if err != nil {
				task.Progress.Status = "error"
				task.Progress.Stage = locale.T("生成失败")
				task.Progress.Error = err.Error()
				return
			}
			for i, j := range a.jobs {
				if j == task {
					a.jobs = append(a.jobs[:i], a.jobs[i+1:]...)
					break
				}
			}
			a.showNotice(locale.T("图片已生成并保存到本机作品库。"))
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

// loadPricing fetches the selected provider's prices once when unset;
// force refetches so model switches pick up remote price changes.
func (a *App) loadPricing(force ...bool) {
	id := a.providerID
	if id == "" || a.priceLoading[id] {
		return
	}
	if len(force) > 0 && force[0] {
		delete(a.pricing, id)
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
					a.showNotice(locale.T("暂时无法读取价格，请先保存服务配置后重试。"))
				} else {
					a.showNotice(locale.T("价格已更新。"))
				}
			}
		}
	})
}

// usdToCNY is the fixed rate for turning a provider's USD quote into the
// RMB amount shown to the user; no live rate is fetched.
const usdToCNY = 6.68

// priceAmount renders a quoted price in RMB: USD converts at the fixed
// rate, CNY passes through, and other currencies keep their code.
func priceAmount(currency string, value float64) string {
	switch currency {
	case "USD":
		value = math.Round(value*usdToCNY*1000) / 1000
		return fmt.Sprintf("¥%g", value)
	case "CNY":
		return fmt.Sprintf("¥%g", value)
	case "JPY":
		return fmt.Sprintf("¥%g", value)
	case "EUR":
		return fmt.Sprintf("€%g", value)
	case "GBP":
		return fmt.Sprintf("£%g", value)
	default:
		return fmt.Sprintf("%s %g", currency, value)
	}
}

func (a *App) priceLabel() string {
	if a.priceLoading[a.providerID] {
		return locale.T("正在查询价格…")
	}
	for _, price := range a.pricing[a.providerID] {
		if price.Model == a.model {
			if value, ok := price.Prices[a.resolution]; ok {
				return fmt.Sprintf(locale.T("%s / 张"), priceAmount(price.Currency, value))
			}
		}
	}
	return ""
}
