package studio

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/locale"
)

// batchEntry is the library header button that opens the batch panel; while
// a batch runs it shows the progress.
func (a *App) batchEntry(c *ui.Context) {
	text := locale.T("批量处理")
	if b := a.batch; b != nil && b.running {
		_, _, done, failed, _ := b.counts()
		text = fmt.Sprintf(locale.T("批量 %d/%d"), done+failed, len(b.items))
	}
	button := quietButton(c, "open-batch", "").Label(locale.T("批量处理")).Tooltip(locale.T("批量处理")).Height(32).Padding(4, 8)
	button.Children(func() {
		ui.Row(c).Center().Gap(8).Children(func() {
			ui.Icon(c, icons["grid"]).Size(16, 16)
			ui.Text(c.Key("batch-entry-text"), text)
		})
	})
	if button.Clicked() {
		a.openBatch()
	}
}

func (a *App) batchPanel(c *ui.Context, narrow bool) {
	if a.batch == nil {
		return
	}
	b := a.batch
	wasOpen := b.open
	restore := popoverTheme(c)
	defer restore()
	width, height := c.Size()
	panelWidth := min(float32(920), max(float32(240), width-48))
	panelHeight := max(float32(360), height-96)
	ui.Modal(c.Key("batch-dialog"), &b.open, func() {
		panel := ui.Column(c.Key("batch-panel")).Width(panelWidth).MaxWidthPercent(100).Height(panelHeight).Gap(12)
		if paths := panel.DroppedFiles(); len(paths) > 0 && !b.running {
			copied := append([]string(nil), paths...)
			a.addBatchFiles(func() ([]string, error) { return a.desktop.CollectImages(copied) })
		}
		panel.Children(func() {
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, locale.T("批量处理")).Grow(1).MinWidth(0).FontSize(textSize(c, 18)).FontWeight(600)
				if iconButton(c, "close-batch", locale.T("关闭批量处理"), "close").Size(32, 32).Clicked() {
					b.open = false
				}
			})
			if a.problem != "" {
				ui.Row(c.Key("batch-problem")).FillWidth().Gap(8).Children(func() {
					ui.Text(c, a.problem).Grow(1).MinWidth(0).TextColor(c.Theme().Danger)
					if iconButton(c, "dismiss-batch-problem", locale.T("关闭提示"), "close").Size(24, 24).Clicked() {
						a.problem = ""
					}
				})
			}
			// On a short window the settings scroll so the list and the start
			// button stay in view.
			ui.Scroll(c.Key("batch-settings")).FillWidth().Shrink(1).MinHeight(64).Gap(10).Disabled(b.running).Children(func() {
				a.batchSources(c)
				a.batchShared(c)
				placeholder := c.Theme().TextMuted
				ui.TextArea(c.Key("batch-prompt"), &b.prompt).Label(locale.T("批量提示词")).Placeholder(locale.T("对每张图要做的处理，例如：把背景换成纯白")).PlaceholderColor(placeholder).Lines(2, 6).FillWidth().Padding(10).FontSize(textSize(c, 14))
				if utf8.RuneCountInString(b.prompt) > 20000 {
					ui.Text(c, locale.T("提示词不能超过 20000 字。")).TextColor(c.Theme().Danger)
				}
				a.batchOptions(c, narrow)
				a.batchOutput(c)
			})
			a.batchList(c)
			a.batchFooter(c)
		})
	})
	if wasOpen && !b.open {
		a.saveBatchPreferences()
	}
	if ui.AlertDialog(c.Key("batch-stop-dialog"), &b.stopOpen, locale.T("取消剩余任务？"), locale.T("正在发送的请求会中断，排队中的图片不再处理。已完成的结果会保留。"), locale.T("继续处理"), locale.T("取消剩余")) == 1 {
		a.stopBatch()
	}
	if ui.AlertDialog(c.Key("batch-clear-dialog"), &b.clearOpen, locale.T("清空列表？"), locale.T("只移除列表中的图片，不会删除原文件和已生成的作品。"), locale.T("取消"), locale.T("清空列表")) == 1 {
		a.clearBatch()
	}
}

func batchLabel(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(textSize(c, 13)).FontWeight(600)
}

func (a *App) batchSources(c *ui.Context) {
	b := a.batch
	ui.Row(c.Key("batch-sources")).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
		batchLabel(c, locale.T("待处理图"))
		ui.Text(c.Key("batch-source-count"), fmt.Sprintf(locale.T("共 %d 张"), len(b.items))).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		if ui.Button(c.Key("batch-add-files"), locale.T("添加图片")).Height(30).Padding(4, 12).Disabled(b.adding).Clicked() {
			a.addBatchFiles(a.desktop.OpenBatchImages)
		}
		if ui.Button(c.Key("batch-add-folder"), locale.T("添加文件夹")).Height(30).Padding(4, 12).Disabled(b.adding).Clicked() {
			a.run(func() func() {
				dir, ok, err := a.desktop.ChooseDirectory(locale.T("选择包含待处理图的文件夹"), "")
				return func() {
					if err != nil {
						a.problem = err.Error()
					} else if ok {
						a.addBatchFiles(func() ([]string, error) { return a.desktop.CollectImages([]string{dir}) })
					}
				}
			})
		}
		if quietButton(c, "batch-clear", locale.T("清空")).Height(30).Padding(4, 10).Disabled(len(b.items) == 0 || b.adding).Clicked() {
			b.clearOpen = true
		}
	})
	if b.adding {
		ui.Text(c, locale.T("正在读取图片…")).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	}
}

func (a *App) batchShared(c *ui.Context) {
	b := a.batch
	ui.Row(c.Key("batch-shared")).FillWidth().Wrap().Gap(10).AlignItems(ui.Center).Children(func() {
		batchLabel(c, fmt.Sprintf(locale.T("共用参考图 %d/%d"), len(b.shared), batchMaxShared))
		for _, reference := range b.shared {
			ref := reference
			ui.Box(c.Key("batch-shared-"+ref.ID)).Size(44, 44).Children(func() {
				if thumb := b.sharedThumbs[ref.ID]; thumb != nil {
					ui.Image(c.Key("batch-shared-image-"+ref.ID), thumb).Fill().Fit(ui.Contain).Radius(8).Border(1, c.Theme().Border).Label(locale.T("共用参考图 ") + ref.Name)
				}
				iconButton(c, "batch-remove-shared-"+ref.ID, locale.T("移除共用参考图 ")+ref.Name, "close").Absolute().Top(-6).Right(-6).Size(20, 20).Background(c.Theme().SurfaceHover).Radius(10).OnClick(func() { a.removeBatchShared(ref.ID) })
			})
		}
		if len(b.shared) < batchMaxShared {
			if ui.Button(c.Key("batch-add-shared"), locale.T("添加")).Label(locale.T("添加共用参考图")).Height(30).Padding(4, 12).Disabled(b.sharing).Clicked() {
				a.addBatchShared(a.desktop.OpenImages)
			}
		}
		ui.Text(c, locale.T("每项请求中，待处理图排第一张，共用参考图依次排在后面。")).Grow(1).Basis(200).MinWidth(0).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	})
}

func (a *App) batchOptions(c *ui.Context, narrow bool) {
	b := a.batch
	control := func(e ui.Element) ui.Element { return e.Height(32).Padding(4, 12).Radius(8) }
	ui.Row(c.Key("batch-options")).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
		labels, _, choices := a.modelChoices()
		current := ""
		for label, choice := range choices {
			if choice.providerID == b.providerID && choice.model == b.model {
				current = label
			}
		}
		model := control(ui.Select(c.Key("batch-model"), &current, labels).Label(locale.T("批量模型"))).Width(240).MaxWidthPercent(100)
		if narrow {
			model.FillWidth()
		}
		if model.Changed() {
			choice := choices[current]
			b.providerID, b.model = choice.providerID, choice.model
			a.reconcileBatch()
			a.loadBatchPricing()
		}
		modes := []string{locale.T("跟随原图比例"), locale.T("指定比例")}
		mode := modes[0]
		if b.sizeMode == "fixed" {
			mode = modes[1]
		}
		if control(ui.Select(c.Key("batch-size-mode"), &mode, modes).Label(locale.T("尺寸方式"))).Width(140).Changed() {
			b.sizeMode = "source"
			if mode == modes[1] {
				b.sizeMode = "fixed"
			}
		}
		if b.sizeMode == "fixed" {
			ratio := ratioLabel(b.ratio)
			ratioWidth := float32(128)
			if locale.Current() == locale.English {
				ratioWidth = 184
			}
			if control(ui.Select(c.Key("batch-ratio"), &ratio, ratioLabels())).Label(locale.T("批量画面比例")).Width(ratioWidth).Changed() {
				b.ratio, _, _ = strings.Cut(ratio, " ")
			}
		}
		resolutions := []string{}
		if p := a.batchProvider(); p != nil {
			for _, r := range p.Resolutions {
				resolutions = append(resolutions, strings.ToUpper(r))
			}
		}
		resolution := strings.ToUpper(b.resolution)
		if control(ui.Select(c.Key("batch-resolution"), &resolution, resolutions)).Label(locale.T("批量分辨率")).Width(88).Changed() {
			b.resolution = strings.ToLower(resolution)
		}
		formats := []string{locale.T("默认格式"), "PNG", "JPEG", "WebP"}
		format := map[string]string{"": formats[0], "png": "PNG", "jpeg": "JPEG", "webp": "WebP"}[b.outputFormat]
		if control(ui.Select(c.Key("batch-format"), &format, formats)).Label(locale.T("批量输出格式")).Width(112).Changed() {
			b.outputFormat = map[string]string{"PNG": "png", "JPEG": "jpeg", "WebP": "webp"}[format]
		}
	})
}

func (a *App) batchOutput(c *ui.Context) {
	b := a.batch
	ui.Row(c.Key("batch-output")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		batchLabel(c, locale.T("输出目录"))
		text := locale.T("未设置，结果只保存在作品库")
		color := c.Theme().TextMuted
		if b.outputDir != "" {
			text, color = b.outputDir, c.Theme().Text
		}
		ui.Text(c.Key("batch-output-path"), text).Grow(1).MinWidth(0).SingleLine().Tooltip(text).FontSize(textSize(c, 13)).TextColor(color)
		if ui.Button(c.Key("batch-choose-output"), locale.T("选择…")).Label(locale.T("选择输出目录")).Height(30).Padding(4, 12).Clicked() {
			a.chooseBatchOutput()
		}
		if b.outputDir != "" && quietButton(c, "batch-clear-output", locale.T("不复制")).Label(locale.T("不复制到输出目录")).Height(30).Padding(4, 10).Clicked() {
			b.outputDir = ""
			a.saveBatchPreferences()
		}
	})
	if b.outputDir != "" {
		settingsNote(c, locale.T("结果照常存入作品库，并按原文件名另存一份到这里；同名时自动加序号，不覆盖已有文件。"))
	}
}

func (a *App) batchList(c *ui.Context) {
	b := a.batch
	if len(b.items) == 0 {
		ui.Column(c.Key("batch-empty")).FillWidth().Grow(1).MinHeight(120).Center().Gap(8).Border(1, c.Theme().Border).Radius(12).Children(func() {
			ui.Text(c, locale.T("还没有待处理图")).FontWeight(500)
			ui.Text(c, locale.T("添加图片或文件夹，也可以直接把文件拖到这里。")).TextAlign(ui.Center).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		})
		return
	}
	b.list.Key = func(i int) any {
		if i < len(b.items) {
			return b.items[i].ID
		}
		return i
	}
	ui.List(c.Key("batch-list"), &b.list, len(b.items), func(i int) {
		a.batchRow(c, b.items[i])
	}).FillWidth().Grow(1).Shrink(0).Basis(0).MinHeight(140).Border(1, c.Theme().Border).Radius(12).Label(locale.T("参数清单"))
}

func (a *App) batchStatus(c *ui.Context, item *batchItem) (string, ui.Color) {
	b := a.batch
	muted := c.Theme().TextMuted
	switch item.Status {
	case batchInvalid:
		return item.Problem, c.Theme().Danger
	case batchReady:
		if _, problem, ok := b.currentPlan(item); ok && problem != "" {
			return problem, c.Theme().Danger
		} else if ok {
			return locale.T("就绪"), muted
		}
		return locale.T("待核对"), muted
	case batchQueued:
		return locale.T("排队中"), muted
	case batchGenerating:
		return fmt.Sprintf(locale.T("生成中 %d%%"), item.Progress), c.Theme().Text
	case batchDone:
		if item.Problem != "" {
			return item.Problem, c.Theme().Danger
		}
		return locale.T("已完成"), c.Theme().Text
	case batchCanceled:
		return locale.T("已取消"), muted
	case batchFailed:
		return item.Problem, c.Theme().Danger
	}
	return "", muted
}

func (a *App) batchRow(c *ui.Context, item *batchItem) {
	b := a.batch
	if item.Status == batchReady {
		// Rows on screen are checked as they appear: problems show before
		// anything is sent, and a long list costs what a screenful does.
		a.planItem(item)
	}
	ui.Column(c.Key("batch-row-"+item.ID)).FillWidth().Padding(8, 10).Gap(8).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(40, 40).Shrink(0).Children(func() {
				if item.Thumb != nil {
					ui.Image(c.Key("batch-thumb-"+item.ID), item.Thumb).Fill().Fit(ui.Contain).Radius(6)
				}
			})
			ui.Column(c).Grow(1).Basis(120).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, item.Name).SingleLine().Tooltip(item.Path).FontSize(textSize(c, 13)).FontWeight(500)
				detail := "—"
				if item.Width > 0 {
					detail = fmt.Sprintf(locale.T("原图 %d×%d · 发送 %s"), item.Width, item.Height, b.request().itemSize(item))
				}
				ui.Text(c, detail).SingleLine().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
			})
			status, color := a.batchStatus(c, item)
			ui.Text(c.Key("batch-status-"+item.ID), status).Basis(160).Shrink(1).MinWidth(0).MaxLines(2).Tooltip(status).FontSize(textSize(c, 12)).TextColor(color).TextAlign(ui.End)
			if item.Status == batchGenerating {
				ui.Progress(c.Key("batch-progress-"+item.ID), float64(item.Progress)/100).Width(60).Label(locale.T("生成进度"))
			}
			if item.Status != batchInvalid {
				label := locale.T("查看请求")
				if item.expanded {
					label = locale.T("收起请求")
				}
				if quietButton(c, "batch-expand-"+item.ID, label).Label(label+" "+item.Name).Height(28).Padding(2, 8).FontSize(textSize(c, 12)).Clicked() {
					item.expanded = !item.expanded
				}
			}
			if item.Status != batchQueued && item.Status != batchGenerating {
				iconButton(c, "batch-remove-"+item.ID, locale.T("移除 ")+item.Name, "close").Size(28, 28).Disabled(b.running).OnClick(func() { a.removeBatchItem(item.ID) })
			}
		})
		if item.expanded {
			a.batchPlanDetail(c, item)
		}
		if item.OutputPath != "" {
			ui.Text(c, locale.T("已另存：")+item.OutputPath).SingleLine().Tooltip(item.OutputPath).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		}
	})
	ui.Divider(c)
}

func formatBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// batchPlanDetail shows the request exactly as it is sent: the key masked
// and images summarised instead of their bytes.
func (a *App) batchPlanDetail(c *ui.Context, item *batchItem) {
	b := a.batch
	plan, problem, ok := b.currentPlan(item)
	if item.Status != batchReady && item.Plan != nil {
		plan, problem, ok = item.Plan, "", true
	}
	ui.Column(c.Key("batch-plan-" + item.ID)).FillWidth().Gap(4).Padding(10).Background(c.Theme().Background).Radius(8).Children(func() {
		muted := c.Theme().TextMuted
		small := textSize(c, 12)
		switch {
		case a.batchProblem() != "" && item.Status == batchReady:
			ui.Text(c, a.batchProblem()).FontSize(small).TextColor(muted)
			return
		case !ok || (plan == nil && problem == ""):
			ui.Text(c, locale.T("正在生成请求预览…")).FontSize(small).TextColor(muted)
			return
		case problem != "":
			ui.Text(c, problem).FontSize(small).TextColor(c.Theme().Danger)
			return
		}
		ui.Text(c, plan.Method+" "+plan.URL).FontSize(small).FontWeight(600)
		auth := locale.T("无")
		if plan.APIKey != "" {
			auth = "Bearer " + plan.APIKey
		}
		ui.Text(c, "Content-Type: "+plan.ContentType+" · Authorization: "+auth).FontSize(small).TextColor(muted)
		for _, field := range plan.Fields {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, field.Name).Width(140).Shrink(0).FontSize(small).TextColor(muted)
				ui.Text(c, field.Value).Grow(1).MinWidth(0).MaxLines(4).FontSize(small)
			})
		}
		for _, image := range plan.Images {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, image.Field).Width(140).Shrink(0).FontSize(small).TextColor(muted)
				ui.Text(c, fmt.Sprintf("%s · %s · %d×%d · %s", image.FileName, image.MIMEType, image.Width, image.Height, formatBytes(image.Bytes))).Grow(1).MinWidth(0).FontSize(small)
			})
		}
	})
}

func (a *App) batchFooter(c *ui.Context) {
	b := a.batch
	ready, invalid, done, failed, waiting := b.counts()
	ui.Row(c.Key("batch-footer")).FillWidth().Wrap().Gap(10).AlignItems(ui.Center).Children(func() {
		var summary string
		if b.running || done+failed > 0 {
			summary = fmt.Sprintf(locale.T("%d/%d 完成 · %d 失败 · %d 等待"), done, len(b.items), failed, waiting)
		} else {
			summary = fmt.Sprintf(locale.T("%d 项可发送 · %d 项有问题"), ready, invalid)
			if price := a.batchPrice(ready); price != "" && ready > 0 {
				summary += " · " + price
			}
		}
		ui.Text(c.Key("batch-summary"), summary).Grow(1).Basis(200).MinWidth(0).FontSize(textSize(c, 13))
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locale.T("并发")).FontSize(textSize(c, 13)).TextColor(c.Theme().TextMuted)
			options := make([]string, batchMaxParallel)
			for i := range options {
				options[i] = fmt.Sprint(i + 1)
			}
			parallel := fmt.Sprint(int(b.parallel))
			if ui.Select(c.Key("batch-parallel"), &parallel, options).Label(locale.T("同时发送的请求数")).Width(72).Height(32).Padding(4, 12).Radius(8).Changed() {
				fmt.Sscan(parallel, &b.parallel)
				a.pumpBatch()
			}
		})
		if !b.running {
			if failed > 0 && ui.Button(c.Key("batch-retry"), fmt.Sprintf(locale.T("重试失败项 %d"), failed)).Height(32).Padding(4, 12).Disabled(a.updateApplying()).Clicked() {
				a.retryBatchFailures()
			}
			start := ui.PrimaryButton(c.Key("batch-start"), fmt.Sprintf(locale.T("开始批量 %d"), ready)).Label(locale.T("开始批量")).Height(32).Padding(4, 14).Disabled(!a.canStartBatch())
			if problem := a.batchProblem(); problem != "" {
				start.Tooltip(problem)
			}
			if start.Clicked() {
				a.startBatch()
			}
			return
		}
		pause := locale.T("暂停")
		if b.paused {
			pause = locale.T("继续")
		}
		if ui.Button(c.Key("batch-pause"), pause).Height(32).Padding(4, 12).Clicked() {
			b.paused = !b.paused
			if b.paused {
				a.showNotice(locale.T("已暂停：正在发送的请求会完成，其余等待继续。"))
			}
			a.pumpBatch()
		}
		if ui.Button(c.Key("batch-stop"), locale.T("取消剩余")).Height(32).Padding(4, 12).Clicked() {
			b.stopOpen = true
		}
	})
}
