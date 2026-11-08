package studio

import (
	"bytes"
	"fmt"
	"github.com/egoist/mygo/ui"
	"image"
	"image/color"
	"image/png"
	"image2ai/internal/domain"
	"math"
	"testing"
	"time"
)

func backendOf(a *App) *testBackend { return a.backend.(*testBackend) }

// Test-only, locally drawn images exercise real decoding and layout. They are
// never seeded into the application or used as pretend generation history.
func visualArtwork(index int) []byte {
	palettes := [][3]color.NRGBA{
		{{220, 220, 202, 255}, {129, 150, 135, 255}, {48, 76, 66, 255}},
		{{227, 205, 180, 255}, {170, 118, 94, 255}, {103, 73, 61, 255}},
		{{202, 219, 224, 255}, {122, 161, 176, 255}, {41, 94, 119, 255}},
		{{231, 217, 185, 255}, {173, 163, 115, 255}, {103, 118, 77, 255}},
		{{213, 211, 225, 255}, {147, 146, 181, 255}, {83, 91, 131, 255}},
		{{223, 213, 193, 255}, {162, 147, 123, 255}, {89, 102, 86, 255}},
	}
	p := palettes[index%len(palettes)]
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			nx, ny := float64(x)/640, float64(y)/480
			v := p[0]
			shade := float64(y) * 0.025
			v.R = uint8(max(0, float64(v.R)-shade))
			v.G = uint8(max(0, float64(v.G)-shade))
			v.B = uint8(max(0, float64(v.B)-shade))
			if math.Pow(nx-0.73, 2)+math.Pow((ny-0.25)*0.75, 2) < 0.004 {
				v = color.NRGBA{244, 236, 211, 255}
			}
			if ny > 0.5+0.12*math.Sin(nx*5+float64(index)) {
				v = p[1]
			}
			if ny > 0.73+0.13*math.Cos(nx*5-float64(index)) {
				v = p[2]
			}
			if index%3 == 0 && x > 265 && x < 350 && y > 260 && y < 333 {
				v = color.NRGBA{225, 215, 189, 255}
				if x > 302 && x < 321 && y > 295 {
					v = p[2]
				}
			}
			if index%3 == 0 && x > 250 && x < 365 && float64(y) > 220+math.Abs(float64(x-307))*0.7 && y < 270 {
				v = p[2]
			}
			img.SetNRGBA(x, y, v)
		}
	}
	var output bytes.Buffer
	_ = png.Encode(&output, img)
	return output.Bytes()
}
func seedVisualGallery(a *App, tt *ui.Tester, b *testBackend) {
	names := []string{"山间小屋 · 几何风景习作", "日落之后 · 暖色风景习作", "雾蓝山脊 · 远山习作", "原野的光 · 配色习作", "暮色边界 · 远山习作", "安静的谷地 · 构图习作"}
	b.mu.Lock()
	b.images = map[string][]byte{}
	b.records = nil
	for i, name := range names {
		id := fmt.Sprintf("visual-%d", i)
		b.images[id] = visualArtwork(i)
		b.records = append(b.records, domain.ImageRecord{ID: id, Prompt: name, ProviderID: "local", ProviderName: "本机模拟服务", Model: "studio-image", Size: "640x480", Width: 640, Height: 480, CreatedAt: time.Date(2026, 11, 5, 10, i*5, 0, 0, time.UTC).UnixMilli(), Favorite: i == 1 || i == 4})
	}
	b.mu.Unlock()
	a.prompt = "一座静谧的山中小屋，晨雾笼罩远山，柔和的自然光，克制的胶片色调。"
	a.refreshHistory()
	settle(a, tt)
}
func TestDesktopKeepsFloatingComposerBelowGallery(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	seedVisualGallery(a, tt, b)
	prompt, ok := tt.Find("提示词")
	if !ok {
		t.Fatal("prompt missing")
	}
	gallery, ok := tt.Find("作品库")
	if !ok {
		t.Fatal("gallery missing")
	}
	picture, ok := tt.Find("预览作品 山间小屋 · 几何风景习作")
	if !ok {
		t.Fatal("visible artwork missing")
	}
	model, modelOK := tt.Find("模型")
	generate, generateOK := tt.Find("开始生成")
	if prompt.W < 900 || prompt.W > 960 || math.Abs(float64(prompt.X+prompt.W/2-640)) > 1 || prompt.Y <= gallery.Y+gallery.H || picture.Y <= gallery.Y || picture.Y >= prompt.Y || !modelOK || !generateOK || model.Y != generate.Y {
		t.Fatalf("centered floating composer must follow gallery with horizontal controls: prompt=%+v model=%+v generate=%+v gallery=%+v artwork=%+v", prompt, model, generate, gallery, picture)
	}
	t.Logf("1280x900: prompt=%+v gallery-title=%+v first-artwork=%+v", prompt, gallery, picture)
}

func TestFloatingComposerStaysPinnedAndFocusHighlightClears(t *testing.T) {
	for _, dark := range []bool{false, true} {
		a, tt, b, _ := fixture(t, 1280, 900)
		tt.SetDark(dark)
		seedVisualGallery(a, tt, b)
		prompt, _ := tt.Find("提示词")
		if grid, ok := tt.Find("作品和生成任务"); !ok || grid.Y+grid.H >= prompt.Y {
			t.Fatalf("gallery must reserve space for the composer: grid=%+v prompt=%+v", grid, prompt)
		}
		x, y := int(prompt.X+prompt.W/2), int(prompt.Y)-1
		border := tt.Image().RGBAAt(x, y)
		click(t, tt, "提示词")
		tt.Frame()
		if tt.Image().RGBAAt(x, y) == border {
			t.Fatalf("focus must highlight the composer border, dark=%v", dark)
		}
		click(t, tt, "搜索作品")
		tt.Frame()
		if tt.Image().RGBAAt(x, y) != border {
			t.Fatalf("composer border must return to rest after focus leaves, dark=%v", dark)
		}
		tt.Scroll(400, 250, 0, 500)
		tt.Frame()
		if after, ok := tt.Find("提示词"); !ok || after != prompt {
			t.Fatalf("scrolling gallery moved the composer: before=%+v after=%+v", prompt, after)
		}
	}
}

func TestGalleryUsesFullWorkspaceWidth(t *testing.T) {
	for _, size := range [][2]int{{375, 812}, {740, 560}, {1280, 900}, {1600, 1000}} {
		t.Run(fmtSize(size), func(t *testing.T) {
			a, tt, b, _ := fixture(t, size[0], size[1])
			seedVisualGallery(a, tt, b)
			grid, gridOK := tt.Find("作品和生成任务")
			gutter := 36
			if size[0] < 600 {
				gutter = 20
			}
			expectedWidth := min(1440, size[0]-gutter*2)
			if !gridOK || math.Abs(float64(grid.W)-float64(expectedWidth)) > 2 {
				t.Fatalf("gallery must retain the full workspace width %d: grid=%+v", expectedWidth, grid)
			}
		})
	}
}

func TestNarrowLibraryKeepsSearchAndSettingsTogether(t *testing.T) {
	_, tt, _, _ := fixture(t, 375, 812)
	search, searchOK := tt.Find("搜索作品")
	settings, settingsOK := tt.Find("打开设置")
	if !searchOK || !settingsOK || search.Y != settings.Y {
		t.Fatalf("settings must stay beside search: search=%+v settings=%+v", search, settings)
	}
}

func TestArtworkCellDoesNotAcquireSelectionBorder(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	seedVisualGallery(a, tt, b)
	cell, ok := tt.Find(a.history.Items[0].Prompt)
	if !ok {
		t.Fatal("artwork cell missing")
	}
	x, y := int(cell.X+cell.W/2), int(cell.Y+1)
	before := tt.Image().RGBAAt(x, y)
	tt.ClickAt(cell.X+cell.W-4, cell.Y+cell.H/2)
	tt.Frame()
	if tt.Image().RGBAAt(x, y) != before {
		t.Fatal("clicking a grid cell must not add a selection border")
	}
}

func TestArtworkImageFrameFillsWidthWithCappedHeight(t *testing.T) {
	for _, size := range [][2]int{{320, 320}, {640, 320}, {320, 640}} {
		a, tt, _, _ := fixture(t, 1280, 900)
		record := domain.ImageRecord{ID: "fit-fixture", Prompt: "图片比例测试", Model: "fixture-image", Width: size[0], Height: size[1]}
		a.history.Items = []domain.ImageRecord{record}
		a.thumbs[record.ID] = ui.NewBitmap(image.NewNRGBA(image.Rect(0, 0, size[0], size[1])))
		tt.Frame()
		r, ok := tt.Find("预览作品 " + record.Prompt)
		if !ok || r.H <= 0 || r.H > artworkMaxImageHeight {
			t.Fatalf("image frame must stay within the compact height for %v: %+v", size, r)
		}
		if r.W != artworkMaxCardWidth-2*artworkCardPadding-2 {
			t.Fatalf("image must fill the card content width: %+v", r)
		}
		button, _ := tt.Find("复用参数 " + record.ID)
		if button.Y-r.Y-r.H > 50 {
			t.Fatalf("caption and actions must not have a flexible empty gap: image=%+v action=%+v", r, button)
		}
	}
}
