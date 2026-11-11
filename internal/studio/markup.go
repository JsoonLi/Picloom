package studio

import (
	"errors"
	"fmt"
	"image"
	"image2ai/internal/locale"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

type markupEditor struct {
	baseMarkup                                      string
	baseBitmap                                      *ui.Bitmap
	exactSize                                       string
	source, prompt, providerID, model, outputFormat string
	ratio, resolution                               string
	picture                                         *ui.Bitmap
	size                                            image.Point
	view                                            imageView
	tool, weight                                    string
	strokes, redo                                   []generation.MarkupStroke
	current                                         generation.MarkupStroke
	drawing, panning, busy                          bool
	direct                                          bool
	last                                            [2]float32
	pointCount                                      int
	problem                                         string
}

func (a *App) beginMarkup() {
	if a.previewLoading || len(a.previewData) == 0 {
		return
	}
	data := a.previewData
	a.loadMarkup(func() ([]byte, error) { return data, nil }, false)
}

func (a *App) loadMarkup(read func() ([]byte, error), direct bool) {
	a.loadMarkupWithBase(read, direct, "")
}

func (a *App) loadMarkupWithBase(read func() ([]byte, error), direct bool, base string) {
	e := &markupEditor{providerID: a.providerID, model: a.model, outputFormat: a.outputFormat, ratio: "auto", resolution: "auto", tool: "pen", weight: "中", busy: true, direct: direct}
	e.baseMarkup = base
	if base != "" {
		e.prompt = a.prompt
		if a.draftSize == "" {
			e.ratio, e.resolution = a.ratio, a.resolution
		} else if a.draftSize != "auto" {
			e.exactSize = a.draftSize
		}
	}
	if e.outputFormat == "gif" || e.outputFormat == "svg" {
		e.outputFormat = "png"
	}
	e.view.reset()
	a.markup = e
	a.run(func() func() {
		data, err := read()
		var source []byte
		var mime string
		var size image.Point
		if err == nil {
			source, mime, size, err = generation.PrepareMarkupSource(data)
		}
		var picture *ui.Bitmap
		var baseBitmap *ui.Bitmap
		var dataURL string
		if err == nil {
			picture, err = ui.DecodeBitmap(source)
			dataURL = generation.DataURL(source, mime)
		}
		if err == nil && base != "" {
			baseBitmap, err = decodeMarkupBase(base, dataURL)
		}
		return func() {
			if a.markup != e || !a.previewOpen {
				return
			}
			e.busy = false
			if direct {
				a.previewLoading = false
			}
			if err != nil {
				e.problem = err.Error()
				return
			}
			e.source, e.picture, e.size = dataURL, picture, size
			e.baseBitmap = baseBitmap
		}
	})
}

func (a *App) markupContent(c *ui.Context, wide bool, contentHeight float32) {
	e := a.markup
	imagePanel := func() {
		a.markupTools(c, e)
		if e.picture == nil {
			label := locale.T("正在准备原图…")
			if e.problem != "" {
				label = e.problem
			}
			ui.Text(c, label).TextColor(c.Theme().TextMuted)
			ui.Spacer(c)
		} else {
			a.markupCanvas(c, e)
		}
	}
	if wide {
		ui.Row(c.Key("markup-content")).Grow(1).MinHeight(0).FillWidth().AlignItems(ui.Stretch).Children(func() {
			ui.Column(c.Key("markup-image-panel")).Grow(1).MinWidth(0).Padding(8).Gap(8).Radius(12).Background(toolbarColor(c)).Children(imagePanel)
			ui.Column(c.Key("markup-instructions-panel")).Width(310).Shrink(0).Padding(0, 0, 0, 20).Gap(16).Children(func() {
				ui.Scroll(c.Key("markup-instructions-scroll")).Grow(1).MinHeight(0).FillWidth().Gap(12).Children(func() { a.markupInstructions(c, e) })
				a.markupActions(c, e)
			})
		})
	} else {
		ui.Scroll(c.Key("markup-content")).Grow(1).MinHeight(0).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(16).Children(func() {
				ui.Column(c.Key("markup-image-panel")).FillWidth().Height(max(float32(300), contentHeight*0.55)).Shrink(0).Padding(8).Gap(8).Radius(12).Background(toolbarColor(c)).Children(imagePanel)
				a.markupInstructions(c, e)
				a.markupActions(c, e)
			})
		})
	}
}

func (a *App) markupTools(c *ui.Context, e *markupEditor) {
	ui.Row(c.Key("markup-tools")).FillWidth().Shrink(0).Wrap().Gap(4).Children(func() {
		for _, tool := range []struct{ id, label string }{{"pen", locale.T("画笔")}, {"rectangle", locale.T("矩形")}, {"ellipse", locale.T("圆圈")}, {"move", locale.T("移动")}} {
			b := quietButton(c, "markup-tool-"+tool.id, tool.label).Label(locale.T("标注工具：")+tool.label).Height(32).Padding(4, 8).Disabled(e.busy)
			if e.tool == tool.id {
				b.Background(c.Theme().SurfacePressed).Border(1, c.Theme().Border)
			}
			if b.Clicked() {
				e.tool = tool.id
			}
		}
		ui.Select(c.Key("markup-weight"), &e.weight, []string{"细", "中", "粗"}).Label("标注线宽").Width(64).MinWidth(0).Height(32).Padding(4, 8).Disabled(e.busy)
		if quietButton(c, "markup-undo", locale.T("撤销")).Label(locale.T("撤销标注")).Height(32).Padding(4, 8).Disabled(e.busy || len(e.strokes) == 0).Clicked() {
			e.undo()
		}
		if quietButton(c, "markup-redo", locale.T("重做")).Label(locale.T("重做标注")).Height(32).Padding(4, 8).Disabled(e.busy || len(e.redo) == 0).Clicked() {
			s := e.redo[len(e.redo)-1]
			e.redo = e.redo[:len(e.redo)-1]
			e.strokes = append(e.strokes, s)
			e.pointCount += len(s.Points)
		}
		if quietButton(c, "markup-clear", locale.T("清空")).Label(locale.T("清空标注")).Height(32).Padding(4, 8).Disabled(e.busy || (len(e.strokes) == 0 && e.baseMarkup == "")).Clicked() {
			e.strokes, e.redo, e.pointCount = nil, nil, 0
			e.baseMarkup, e.baseBitmap = "", nil
			e.problem = ""
		}
	})
}

func (a *App) markupInstructions(c *ui.Context, e *markupEditor) {
	ui.Text(c, locale.T("圈出要改的位置")).FontSize(textSize(c, 16)).FontWeight(600)
	ui.Text(c, locale.T("用红色标注位置，再写下修改要求。")).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	if e.baseMarkup != "" {
		ui.Text(c, locale.T("已保留原标注，可追加或清空。")).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	}
	ui.TextArea(c.Key("markup-prompt"), &e.prompt).Label(locale.T("标注修改要求")).Placeholder(locale.T("例如：把红圈内的房子改成木屋，保留其余画面。")).Lines(4, 8).MinHeight(128).FillWidth().Padding(12).Disabled(e.busy)
	labels, _, choices := a.modelChoices()
	current := ""
	for label, choice := range choices {
		if choice.providerID == e.providerID && choice.model == e.model {
			current = label
		}
	}
	if ui.Select(c.Key("markup-model"), &current, labels).Label(locale.T("标注编辑模型")).FillWidth().Height(36).Disabled(e.busy).Changed() {
		choice := choices[current]
		e.providerID, e.model = choice.providerID, choice.model
		if e.resolution != "auto" {
			e.resolution = providerDefaultResolution(a.markupProvider(e))
		}
	}
	ui.Text(c, locale.T("使用 GPT Image 2.6\n请确认渠道支持标注编辑。")).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	a.markupSizeControls(c, e)
	format := strings.ToUpper(e.outputFormat)
	if format == "" {
		format = locale.T("默认格式")
	} else if format == "WEBP" {
		format = "WebP"
	}
	if ui.Select(c.Key("markup-output-format"), &format, []string{locale.T("默认格式"), "PNG", "JPEG", "WebP"}).Label(locale.T("标注编辑输出格式")).FillWidth().Height(36).Disabled(e.busy).Changed() {
		e.outputFormat = strings.ToLower(format)
		if format == locale.T("默认格式") {
			e.outputFormat = ""
		}
	}
	if e.picture != nil {
		status := fmt.Sprintf(locale.T("%d 处标注"), len(e.strokes))
		if e.baseMarkup != "" {
			status = locale.T("已保留原标注")
		}
		ui.Text(c, fmt.Sprintf(locale.T("原图 %d × %d px · %s"), e.size.X, e.size.Y, status)).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	}
	if size, err := e.requestSize(); err == nil {
		label := locale.T("输出尺寸：自动")
		if size != "auto" {
			label = locale.T("输出 ") + strings.ReplaceAll(size, "x", " × ") + " px"
		}
		ui.Text(c.Key("markup-output-size"), label).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	} else {
		ui.Text(c.Key("markup-size-error"), err.Error()).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().Danger)
	}
	ui.Text(c, locale.T("生成结果另存作品库。")).FillWidth().FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	if len(utf16.Encode([]rune(e.prompt))) > 20000 {
		ui.Text(c, locale.T("修改要求不能超过 20000 字。")).TextColor(c.Theme().Danger)
	}
	if e.problem != "" {
		ui.Text(c.Key("markup-error"), e.problem).TextColor(c.Theme().Danger)
	}
}

func (a *App) markupProvider(e *markupEditor) *domain.Provider {
	for i := range a.config.Providers {
		if a.config.Providers[i].ID == e.providerID && !a.config.Providers[i].Disabled {
			return &a.config.Providers[i]
		}
	}
	return nil
}

func (a *App) markupSizeControls(c *ui.Context, e *markupEditor) {
	p := a.markupProvider(e)
	ui.Row(c.Key("markup-size-controls")).FillWidth().Gap(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, locale.T("画面比例")).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
			ratio := ratioLabel(e.ratio)
			switch ratio {
			case "auto":
				ratio = locale.T("自动")
			case "source":
				ratio = locale.T("原图比例")
			}
			options := append([]string{locale.T("自动"), locale.T("原图比例")}, ratioLabels()...)
			if e.exactSize != "" {
				ratio, options = locale.T("沿用"), append([]string{locale.T("沿用")}, options...)
			}
			if ui.Select(c.Key("markup-ratio"), &ratio, options).Label(locale.T("标注编辑画面比例")).FillWidth().MinWidth(0).Height(36).Disabled(e.busy).Changed() && ratio != locale.T("沿用") {
				e.exactSize = ""
				switch ratio {
				case locale.T("自动"):
					e.ratio, e.resolution = "auto", "auto"
				case locale.T("原图比例"):
					e.ratio = "source"
				default:
					e.ratio, _, _ = strings.Cut(ratio, " ")
				}
				if e.ratio != "auto" && e.resolution == "auto" {
					e.resolution = providerDefaultResolution(p)
				}
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, locale.T("分辨率")).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
			resolution := strings.ToUpper(e.resolution)
			if e.resolution == "auto" {
				resolution = locale.T("自动")
			}
			options := []string{locale.T("自动")}
			if p != nil {
				for _, value := range p.Resolutions {
					options = append(options, strings.ToUpper(value))
				}
			}
			if e.exactSize != "" {
				resolution, options = locale.T("沿用"), append([]string{locale.T("沿用")}, options...)
			}
			if ui.Select(c.Key("markup-resolution"), &resolution, options).Label(locale.T("标注编辑分辨率")).FillWidth().MinWidth(0).Height(36).Disabled(e.busy).Changed() && resolution != locale.T("沿用") {
				e.exactSize = ""
				if resolution == locale.T("自动") {
					e.ratio, e.resolution = "auto", "auto"
				} else {
					e.resolution = strings.ToLower(resolution)
					if e.ratio == "auto" {
						e.ratio = "source"
					}
				}
			}
		})
	})
}

// Output dimensions never resize the source or its annotation coordinate space.
func (e *markupEditor) requestSize() (string, error) {
	if e.exactSize != "" {
		var w, h int
		if _, err := fmt.Sscanf(e.exactSize, "%dx%d", &w, &h); err != nil || fmt.Sprintf("%dx%d", w, h) != e.exactSize || w < 256 || h < 256 || w > 8192 || h > 8192 || w%16 != 0 || h%16 != 0 {
			return "", errors.New(locale.T("原输出尺寸无效，请重新选择画面比例或分辨率。"))
		}
		return e.exactSize, nil
	}
	if e.ratio == "auto" && e.resolution == "auto" {
		return "auto", nil
	}
	if e.resolution != "2k" && e.resolution != "4k" {
		return "", errors.New(locale.T("请选择有效的输出分辨率。"))
	}
	ratio := e.ratio
	if ratio == "source" {
		if e.size.X <= 0 || e.size.Y <= 0 {
			return "", errors.New(locale.T("请等原图读取完成后选择尺寸。"))
		}
		ratio = fmt.Sprintf("%d:%d", e.size.X, e.size.Y)
	} else if !contains(ratios, ratio) {
		return "", errors.New(locale.T("请选择有效的画面比例。"))
	}
	size := generation.ComputeSize(ratio, e.resolution)
	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || width > 8192 || height > 8192 {
		return "", errors.New(locale.T("此比例的输出边长超过 8192 像素，请降低分辨率或选择其他比例。"))
	}
	return size, nil
}

func (a *App) markupActions(c *ui.Context, e *markupEditor) {
	ui.Row(c.Key("markup-actions")).FillWidth().Shrink(0).Gap(8).Children(func() {
		if ui.Button(c.Key("markup-cancel"), locale.T("取消标注")).Height(40).Clicked() {
			a.markup = nil
			if e.direct {
				a.previewOpen = false
			}
		}
		label := locale.T("开始生成")
		if e.busy {
			label = locale.T("准备中…")
		}
		if ui.PrimaryButton(c.Key("markup-submit"), label).Label(locale.T("生成标注编辑")).Height(40).Grow(1).Disabled(!e.canSubmit()).Clicked() {
			a.submitMarkup(e)
		}
	})
}

func (e *markupEditor) canSubmit() bool {
	_, sizeErr := e.requestSize()
	return sizeErr == nil && !e.busy && !e.drawing && e.picture != nil && (len(e.strokes) > 0 || e.baseMarkup != "") && strings.TrimSpace(e.prompt) != "" && len(utf16.Encode([]rune(e.prompt))) <= 20000 && e.providerID != "" && e.model != ""
}

func (a *App) submitMarkup(e *markupEditor) {
	if a.updateApplying() || !e.canSubmit() {
		return
	}
	e.busy, e.problem = true, ""
	strokes := make([]generation.MarkupStroke, len(e.strokes))
	for i, s := range e.strokes {
		strokes[i] = s
		strokes[i].Points = append([][2]float32(nil), s.Points...)
	}
	outputSize, _ := e.requestSize() // canSubmit already validated this snapshot.
	args := domain.GenerateArgs{JobID: newID(), Prompt: strings.TrimSpace(e.prompt), ProviderID: e.providerID, Model: e.model, Size: outputSize, OutputFormat: e.outputFormat, ReferenceImages: []string{e.source}}
	name := ""
	for _, provider := range a.config.Providers {
		if provider.ID == e.providerID {
			name = provider.Name
		}
	}
	a.run(func() func() {
		markup, err := renderMarkupOverlay(e.size, strokes, e.baseMarkup)
		args.Markup = markup
		return func() {
			if a.markup != e || !a.previewOpen {
				return
			}
			e.busy = false
			if err != nil {
				e.problem = err.Error()
				return
			}
			a.startJob(args, name)
			a.previewOpen = false
		}
	})
}

func (e *markupEditor) undo() {
	if len(e.strokes) > 0 {
		s := e.strokes[len(e.strokes)-1]
		e.strokes = e.strokes[:len(e.strokes)-1]
		e.redo = append(e.redo, s)
		e.pointCount -= len(s.Points)
		e.problem = ""
	}
}

func (a *App) markupCanvas(c *ui.Context, e *markupEditor) {
	view := &e.view
	iw, ih := float32(e.size.X), float32(e.size.Y)
	box := ui.Box(c.Key("markup-canvas")).FillWidth().Grow(1).MinHeight(100).Clip().Focusable().Label(locale.T("图片标注画布"))
	bounds := box.Bounds()
	w, h := max(float32(1), bounds.W), max(float32(1), bounds.H)
	ww, wh := c.Size()
	if bounds.W == 0 || bounds.H == 0 || view.windowWidth != ww || view.windowHeight != wh {
		c.After(0)
		view.windowWidth, view.windowHeight = ww, wh
	}
	if view.fitted {
		view.scale, view.x, view.y = fitImage(w, h, iw, ih), 0, 0
	} else {
		view.constrain(w, h, iw, ih)
	}
	box.HandleInput(func(ev ui.InputEvent) bool { return e.input(ev, w, h) })
	if !e.busy && box.Shortcut(ui.Cmd, ui.KeyZ) {
		e.undo()
	}
	base, shade := c.Theme().Surface, c.Theme().Border
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		target := e.target(r.W, r.H)
		target.X += r.X
		target.Y += r.Y
		p.Clip(r, 0, func() {
			drawTransparencyGrid(p, target, r, base, shade)
			p.Image(e.picture, target, ui.FillBox)
			p.Clip(target, 0, func() {
				if e.baseBitmap != nil {
					p.Image(e.baseBitmap, target, ui.FillBox)
				}
				for _, s := range e.strokes {
					drawMarkup(p, target, iw, s)
				}
				if e.drawing {
					drawMarkup(p, target, iw, e.current)
				}
			})
		})
	})
	ui.Row(c.Key("markup-zoom-tools")).FillWidth().Shrink(0).Wrap().Gap(4).AlignItems(ui.Center).Children(func() {
		for _, item := range []struct {
			key, label string
			factor     float32
		}{{"out", "−", 0.8}, {"in", "+", 1.25}} {
			if quietButton(c, "markup-zoom-"+item.key, item.label).Label(locale.T("标注画布")+map[string]string{"in": locale.T("放大"), "out": locale.T("缩小")}[item.key]).Size(32, 32).Padding(0).Disabled(e.busy).Clicked() {
				view.zoom(view.scale*item.factor, 0, 0, w, h, iw, ih)
			}
		}
		if quietButton(c, "markup-fit", locale.T("适应窗口")).Label(locale.T("标注画布适应窗口")).Height(32).Padding(4, 8).Disabled(e.busy).Clicked() {
			view.reset()
		}
		ui.Text(c, fmt.Sprintf("%.0f%%", view.scale*100)).FontSize(12).TextColor(c.Theme().TextMuted)
	})
}

func (e *markupEditor) target(w, h float32) ui.Rect {
	scale, x, y := e.view.scale, e.view.x, e.view.y
	if e.view.fitted {
		scale, x, y = fitImage(w, h, float32(e.size.X), float32(e.size.Y)), 0, 0
	}
	return ui.Rect{X: w/2 + x - float32(e.size.X)*scale/2, Y: h/2 + y - float32(e.size.Y)*scale/2, W: float32(e.size.X) * scale, H: float32(e.size.Y) * scale}
}

func (e *markupEditor) input(ev ui.InputEvent, w, h float32) bool {
	if e.busy {
		return true
	}
	iw, ih := float32(e.size.X), float32(e.size.Y)
	target := e.target(w, h)
	point := [2]float32{max(0, min(iw, (ev.X-target.X)*iw/target.W)), max(0, min(ih, (ev.Y-target.Y)*ih/target.H))}
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if e.tool == "move" {
			e.panning, e.last = true, [2]float32{ev.X, ev.Y}
			return true
		}
		if ev.X < target.X || ev.Y < target.Y || ev.X > target.X+target.W || ev.Y > target.Y+target.H {
			return true
		}
		if len(e.strokes) >= generation.MaxMarkupStrokes || e.pointCount >= generation.MaxMarkupPoints-2 {
			e.problem = locale.T("标注已达上限，请撤销或清空部分标注。")
			return true
		}
		factor := map[string]float32{"细": 0.003, "中": 0.007, "粗": 0.014}[e.weight]
		width := min(max(float32(1), min(iw, ih)*factor), float32(64))
		e.current = generation.MarkupStroke{Tool: e.tool, Points: [][2]float32{point}, Width: width}
		e.drawing, e.problem = true, ""
		return true
	case ui.InputPointerMove, ui.InputPointerUp:
		if e.panning {
			e.view.fitted = false
			e.view.x += ev.X - e.last[0]
			e.view.y += ev.Y - e.last[1]
			e.view.constrain(w, h, iw, ih)
			e.last = [2]float32{ev.X, ev.Y}
			if ev.Kind == ui.InputPointerUp {
				e.panning = false
			}
			return true
		}
		if !e.drawing {
			return false
		}
		points := e.current.Points
		if e.current.Tool != "pen" {
			e.current.Points = append(points[:1], point)
		} else if e.pointCount+len(points) < generation.MaxMarkupPoints {
			last := points[len(points)-1]
			if math.Hypot(float64(point[0]-last[0]), float64(point[1]-last[1]))*float64(e.view.scale) >= 0.75 || ev.Kind == ui.InputPointerUp {
				e.current.Points = append(points, point)
			}
		} else {
			e.problem = locale.T("标注已达上限，请撤销部分标注后继续。")
		}
		if ev.Kind == ui.InputPointerUp {
			e.strokes = append(e.strokes, e.current)
			e.pointCount += len(e.current.Points)
			e.current, e.redo, e.drawing = generation.MarkupStroke{}, nil, false
		}
		return true
	case ui.InputScroll, ui.InputZoom:
		if e.drawing || e.panning {
			return true
		}
		factor := ev.Scale
		if ev.Kind == ui.InputScroll {
			factor = float32(math.Exp(-math.Max(-300, math.Min(300, float64(ev.DY))) * 0.002))
		}
		e.view.zoom(e.view.scale*factor, ev.X-w/2, ev.Y-h/2, w, h, iw, ih)
		return true
	}
	return false
}

func drawMarkup(p *ui.Painter, target ui.Rect, iw float32, stroke generation.MarkupStroke) {
	scale := target.W / iw
	points := generation.MarkupPolyline(stroke)
	transformed := make([][2]float32, len(points))
	for i, point := range points {
		transformed[i] = [2]float32{target.X + point[0]*scale, target.Y + point[1]*scale}
	}
	color := generation.MarkupColor
	ink := ui.RGBA(color.R, color.G, color.B, 1)
	width := stroke.Width * scale
	if len(transformed) == 1 {
		p.Fill(ui.Rect{X: transformed[0][0] - width/2, Y: transformed[0][1] - width/2, W: width, H: width}, ink, width/2)
	} else {
		p.StrokePolyline(transformed, width, ink)
	}
}
