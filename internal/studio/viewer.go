package studio

import "image2ai/internal/locale"
import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// imageView uses image pixels as its scale, preserving the legacy viewer's
// fit/1:1 modes, detail-anchored wheel zoom and constrained panning.
type imageView struct {
	scale, x, y               float32
	fitted                    bool
	windowWidth, windowHeight float32
}

func (v *imageView) reset() { *v = imageView{scale: 1, fitted: true} }
func fitImage(vw, vh, iw, ih float32) float32 {
	if iw <= 0 || ih <= 0 {
		return 1
	}
	return min(float32(1), vw/iw, vh/ih)
}
func (v *imageView) constrain(vw, vh, iw, ih float32) {
	v.scale = max(min(float32(0.1), fitImage(vw, vh, iw, ih)), min(float32(8), v.scale))
	lx, ly := max(float32(0), (iw*v.scale-vw)/2), max(float32(0), (ih*v.scale-vh)/2)
	v.x = max(-lx, min(lx, v.x))
	v.y = max(-ly, min(ly, v.y))
}
func (v *imageView) zoom(scale, x, y, vw, vh, iw, ih float32) {
	if v.fitted {
		v.scale = fitImage(vw, vh, iw, ih)
		v.fitted = false
	}
	old := v.scale
	v.scale = scale
	v.constrain(vw, vh, iw, ih)
	if old > 0 {
		factor := v.scale / old
		v.x = x - (x-v.x)*factor
		v.y = y - (y-v.y)*factor
	}
	v.constrain(vw, vh, iw, ih)
}

func (a *App) imageViewer(c *ui.Context) {
	picture := a.animatedPreview(c)
	iw, ih := ui.ImageSize(picture)
	if a.previewWidth > 0 && a.previewHeight > 0 {
		// A thumbnail placeholder lays out at the original's size, so the
		// view does not jump when the full image replaces it.
		iw, ih = a.previewWidth, a.previewHeight
	}
	view := &a.viewer
	box := ui.Box(c.Key("preview-image")).FillWidth().Grow(1).MinHeight(100).Clip().Focusable().Label(locale.T("图片缩放查看器"))
	// Use the laid-out image well so zoom anchors and pan limits follow the
	// available space, including wrapped controls and system text scaling.
	bounds := box.Bounds()
	width, height := max(float32(1), bounds.W), max(float32(1), bounds.H)
	windowWidth, windowHeight := c.Size()
	if bounds.W == 0 || bounds.H == 0 || view.windowWidth != windowWidth || view.windowHeight != windowHeight {
		// Bounds describes the previous frame. Rebuild once after opening or
		// resizing so the percentage and input geometry use the new layout.
		c.After(0)
		view.windowWidth, view.windowHeight = windowWidth, windowHeight
	}
	if view.fitted {
		view.scale = fitImage(width, height, iw, ih)
		view.x, view.y = 0, 0
	} else {
		view.constrain(width, height, iw, ih)
	}
	box.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputZoom:
			view.zoom(view.scale*ev.Scale, ev.X-width/2, ev.Y-height/2, width, height, iw, ih)
			return true
		case ui.InputScroll:
			factor := float32(math.Exp(-math.Max(-300, math.Min(300, float64(ev.DY))) * 0.002))
			view.zoom(view.scale*factor, ev.X-width/2, ev.Y-height/2, width, height, iw, ih)
			return true
		default:
			return false
		}
	})
	if dx, dy, dragging := box.Dragged(); dragging {
		view.fitted = false
		view.x += dx
		view.y += dy
		view.constrain(width, height, iw, ih)
	}
	if box.DoubleClicked() {
		if view.fitted {
			view.zoom(1, 0, 0, width, height, iw, ih)
		} else {
			view.reset()
		}
	}
	if box.Shortcut(0, ui.KeyEqual) || box.Shortcut(ui.Shift, ui.KeyEqual) {
		view.zoom(view.scale*1.25, 0, 0, width, height, iw, ih)
	}
	if box.Shortcut(0, ui.KeyMinus) {
		view.zoom(view.scale/1.25, 0, 0, width, height, iw, ih)
	}
	if box.Shortcut(0, ui.Key0) {
		view.reset()
	}
	if box.Shortcut(0, ui.Key1) {
		view.zoom(1, 0, 0, width, height, iw, ih)
	}
	for _, arrow := range []struct {
		key  ui.Key
		x, y float32
	}{{ui.KeyLeft, 40, 0}, {ui.KeyRight, -40, 0}, {ui.KeyUp, 0, 40}, {ui.KeyDown, 0, -40}} {
		if box.Shortcut(0, arrow.key) {
			view.fitted = false
			view.x += arrow.x
			view.y += arrow.y
			view.constrain(width, height, iw, ih)
		}
		if box.Shortcut(ui.Shift, arrow.key) {
			view.fitted = false
			view.x += arrow.x * 2.5
			view.y += arrow.y * 2.5
			view.constrain(width, height, iw, ih)
		}
	}
	checkerBase, checkerShade := c.Theme().Surface, c.Theme().Border
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		scale := view.scale
		x, y := view.x, view.y
		if view.fitted {
			scale = fitImage(r.W, r.H, iw, ih)
			x, y = 0, 0
		}
		target := ui.Rect{X: r.X + r.W/2 + x - iw*scale/2, Y: r.Y + r.H/2 + y - ih*scale/2, W: iw * scale, H: ih * scale}
		p.Clip(r, 0, func() {
			drawTransparencyGrid(p, target, r, checkerBase, checkerShade)
			p.Image(picture, target, ui.FillBox)
		})
	})
	ui.Row(c.Key("viewer-controls")).FillWidth().Shrink(0).Wrap().Gap(4).AlignItems(ui.Center).Children(func() {
		if animation := a.previewAnimation; animation != nil {
			label := locale.T("暂停")
			if animation.paused {
				label = locale.T("播放")
			}
			if _, next := animation.frame(c.Now()); next == 0 && !animation.paused {
				animation.elapsed = animation.position(c.Now())
				animation.paused = true
				label = locale.T("重播")
			}
			if quietButton(c, "preview-play", label).Label(locale.T("播放或暂停 GIF")).Height(32).Padding(4, 8).Clicked() {
				animation.toggle(c.Now())
			}
		}
		if quietButton(c, "zoom-out", "−").Label(locale.T("缩小")).Tooltip(locale.T("缩小")).Size(32, 32).Padding(0).Clicked() {
			view.zoom(view.scale/1.25, 0, 0, width, height, iw, ih)
		}
		if quietButton(c, "zoom-in", "+").Label(locale.T("放大")).Tooltip(locale.T("放大")).Size(32, 32).Padding(0).Clicked() {
			view.zoom(view.scale*1.25, 0, 0, width, height, iw, ih)
		}
		if quietButton(c, "zoom-fit", locale.T("适应窗口")).Height(32).Padding(4, 8).Clicked() {
			view.reset()
		}
		if quietButton(c, "zoom-actual", locale.T("原始大小")).Height(32).Padding(4, 8).Clicked() {
			view.zoom(1, 0, 0, width, height, iw, ih)
		}
		ui.Text(c, fmt.Sprintf("%.0f%%", view.scale*100)).FontSize(12).TextColor(c.Theme().TextMuted)
		if a.previewEditable {
			ui.Spacer(c)
			if quietButton(c, "open-markup", locale.T("标注编辑")).Label(locale.T("标注编辑当前图片")).Height(32).Padding(4, 8).Disabled(a.previewLoading || len(a.previewData) == 0).Clicked() {
				a.beginMarkup()
			}
		}
	})
}

// Paint only the visible image bounds, keeping the work bounded even at 8x
// zoom. The bitmap covers the grid everywhere its pixels are opaque.
func drawTransparencyGrid(p *ui.Painter, image, viewport ui.Rect, base, shade ui.Color) {
	left, top := max(image.X, viewport.X), max(image.Y, viewport.Y)
	right, bottom := min(image.X+image.W, viewport.X+viewport.W), min(image.Y+image.H, viewport.Y+viewport.H)
	if right <= left || bottom <= top {
		return
	}
	visible := ui.Rect{X: left, Y: top, W: right - left, H: bottom - top}
	const tile = float32(12)
	p.Clip(visible, 0, func() {
		p.Fill(visible, base, 0)
		firstColumn := int(math.Floor(float64((left - image.X) / tile)))
		firstRow := int(math.Floor(float64((top - image.Y) / tile)))
		for row := firstRow; image.Y+float32(row)*tile < bottom; row++ {
			for column := firstColumn; image.X+float32(column)*tile < right; column++ {
				if (row+column)%2 == 0 {
					p.Fill(ui.Rect{X: image.X + float32(column)*tile, Y: image.Y + float32(row)*tile, W: tile, H: tile}, shade, 0)
				}
			}
		}
	})
}
