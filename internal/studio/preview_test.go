package studio

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
)

func TestPreviewUsesAvailableWindow(t *testing.T) {
	for _, size := range [][2]int{{1600, 1100}, {1280, 900}, {740, 560}, {375, 812}} {
		for _, dark := range []bool{false, true} {
			theme := "light"
			if dark {
				theme = "dark"
			}
			name := fmtSize(size) + "-" + theme
			t.Run(name, func(t *testing.T) {
				a, tt, b, _ := fixture(t, size[0], size[1])
				tt.SetDark(dark)
				seedVisualGallery(a, tt, b)
				record := a.history.Items[0]
				record.ReferenceCount = 1
				record.MIMEType = "image/png"
				a.openArtwork(record)
				settle(a, tt)
				viewer, ok := tt.Find("图片缩放查看器")
				if !ok || viewer.X < 0 || viewer.Y < 0 || viewer.X+viewer.W > float32(size[0]) || viewer.Y+viewer.H > float32(size[1]) {
					t.Fatalf("preview must fit in the window: %+v", viewer)
				}
				if size[0] >= 900 {
					title, titleFound := tt.Find("作品详情")
					close, closeFound := tt.Find("关闭")
					// The close button and text form one centered header row;
					// font metrics vary across platforms, so measure its bottom.
					headerBottom := max(title.Y+title.H, close.Y+close.H)
					sameRow := math.Abs(float64(title.Y+title.H/2-close.Y-close.H/2)) <= 1
					if !titleFound || !closeFound || !sameRow || close.X+close.W-viewer.X+28 > previewMaxWidth+1 || viewer.Y-headerBottom > 28 {
						t.Fatalf("preview must retain its width cap and compact header: viewer=%+v title=%+v close=%+v", viewer, title, close)
					}
					frame := tt.Image()
					if frame.RGBAAt(int(viewer.X+1), int(viewer.Y+1)) != frame.RGBAAt(int(viewer.X-4), int(viewer.Y+1)) {
						t.Fatal("image well must share the preview panel background")
					}
					if viewer.H < float32(size[1])*0.7 || viewer.W < min(float32(size[0]-2*previewWindowInset), float32(previewMaxWidth))-400 {
						t.Fatalf("preview must expand with the window: %+v", viewer)
					}
					controls, _ := tt.Find("适应窗口")
					action, found := tt.Find("复用参数")
					if !found || action.Y+action.H > float32(size[1])-20 || action.Y+action.H < controls.Y {
						t.Fatalf("actions must stay at the bottom beside the viewer tools: action=%+v tools=%+v", action, controls)
					}
				} else {
					tt.Scroll(viewer.X+10, viewer.Y+viewer.H+20, 0, 2000)
					tt.Frame()
					action, found := tt.Find("复用参数")
					if !found || action.Y < 0 || action.Y+action.H > float32(size[1])-20 {
						t.Fatalf("stacked preview actions must be reachable by scrolling: %+v", action)
					}
				}
				if tt.HasText("滚轮缩放 · 拖拽移动 · 双击切换原始大小") {
					t.Fatal("preview must omit the footer instructions")
				}
				if output := os.Getenv("IMAGE2AI_PREVIEW_SNAPSHOTS"); output != "" {
					if err := os.MkdirAll(output, 0700); err != nil {
						t.Fatal(err)
					}
					file, err := os.Create(filepath.Join(output, name+".png"))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, tt.Image())
					file.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestPreviewDistinguishesTransparencyFromWhite(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a, tt, b, _ := fixture(t, 1280, 900)
		tt.SetDark(dark)
		picture := image.NewNRGBA(image.Rect(0, 0, 128, 128))
		draw.Draw(picture, image.Rect(64, 0, 128, 128), image.NewUniform(color.White), image.Point{}, draw.Src)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, picture); err != nil {
			t.Fatal(err)
		}
		b.mu.Lock()
		b.images = map[string][]byte{"alpha": encoded.Bytes()}
		b.mu.Unlock()
		a.openArtwork(domain.ImageRecord{ID: "alpha", Prompt: "左侧透明，右侧白色", MIMEType: "image/png", Size: "128x128"})
		settle(a, tt)
		viewer, _ := tt.Find("图片缩放查看器")
		x, y := int(viewer.X+viewer.W/2-64), int(viewer.Y+viewer.H/2-64)
		frame := tt.Image()
		if frame.RGBAAt(x+3, y+3) == frame.RGBAAt(x+15, y+3) {
			t.Fatalf("transparent pixels must reveal the checkerboard, dark=%v", dark)
		}
		for _, offset := range []int{80, 92} {
			if frame.RGBAAt(x+offset, y+3) != (color.RGBA{255, 255, 255, 255}) {
				t.Fatalf("opaque white must cover the checkerboard, dark=%v", dark)
			}
		}
		if frame.RGBAAt(x-8, y+3) != frame.RGBAAt(int(viewer.X-4), y+3) {
			t.Fatal("checkerboard must not extend outside the image")
		}
		if output := os.Getenv("IMAGE2AI_PREVIEW_SNAPSHOTS"); output != "" {
			file, err := os.Create(filepath.Join(output, map[bool]string{false: "alpha-light.png", true: "alpha-dark.png"}[dark]))
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
