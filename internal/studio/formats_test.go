package studio

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"image2ai/internal/domain"
)

func TestOutputFormatSurvivesSettingsAndReachesGeneration(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {375, 812}} {
		a, tt, b, _ := fixture(t, size[0], size[1])
		if control, ok := tt.Find("输出格式"); !ok || control.X < 0 || control.X+control.W > float32(size[0]) || control.Y+control.H > float32(size[1]) {
			t.Fatal("format selector missing")
		}
		click(t, tt, "输出格式")
		click(t, tt, "GIF")
		settle(a, tt)
		a.openSettings()
		tt.Frame()
		a.settingsOpen = false
		tt.Frame()
		if a.outputFormat != "gif" || b.config.GenerationPreferences.OutputFormat != "gif" {
			t.Fatal("format not preserved")
		}
		b.generateStarted = make(chan domain.GenerateArgs, 1)
		a.prompt = "动图"
		a.generate()
		args := <-b.generateStarted
		if args.OutputFormat != "gif" {
			t.Fatal("format missing from request")
		}
		settle(a, tt)
	}
}

func TestGIFComposerSnapshots(t *testing.T) {
	output := os.Getenv("IMAGE2AI_GIF_SNAPSHOTS")
	if output == "" {
		t.Skip("set IMAGE2AI_GIF_SNAPSHOTS to capture GIF controls")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}} {
		for _, dark := range []bool{false, true} {
			a, tt, _, _ := fixture(t, size[0], size[1])
			tt.SetDark(dark)
			a.outputFormat = "gif"
			a.gifFrameCount = 12
			a.gifFrameDurationMS = 100
			a.prompt = "一只像素猫眨眼的循环动画"
			tt.Frame()
			theme := "light"
			if dark {
				theme = "dark"
			}
			path := filepath.Join(output, "gif-controls-"+theme+"-"+fmtSize(size)+".png")
			file, err := os.Create(path)
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

func TestGIFToolbarFitsSingleRowAtMaximumWidth(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {1920, 1080}} {
		a, tt, _, _ := fixture(t, size[0], size[1])
		a.outputFormat = "gif"
		tt.Frame()
		reference, _ := tt.Find("添加参考图")
		for _, label := range []string{"模型", "GIF 单帧尺寸", "输出格式", "GIF 帧数", "GIF 帧率", "开始生成"} {
			control, ok := tt.Find(label)
			if !ok || control.Y != reference.Y || control.X+control.W > float32(size[0]) {
				t.Fatalf("%v: %s must share reference row %+v, got %+v", size, label, reference, control)
			}
			t.Logf("%v %s: %+v", size, label, control)
		}
	}
}

func TestGIFPreviewPlaysPausesAndReleasesFrames(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	palette := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	first := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var data bytes.Buffer
	if err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{5, 10}, LoopCount: 0}); err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	b.images = map[string][]byte{"animated": data.Bytes()}
	b.mu.Unlock()
	a.openArtwork(domain.ImageRecord{ID: "animated", Prompt: "两帧动图", MIMEType: "image/gif", Width: 8, Height: 8})
	settle(a, tt)
	if a.previewAnimation == nil {
		t.Fatalf("animation missing: %s", a.problem)
	}
	well, ok := tt.Find("图片缩放查看器")
	if !ok {
		t.Fatal("viewer missing")
	}
	x, y := int(well.X+well.W/2), int(well.Y+well.H/2)
	before := tt.Image().RGBAAt(x, y)
	tt.Advance(60 * time.Millisecond)
	after := tt.Image().RGBAAt(x, y)
	if before == after {
		t.Fatal("GIF did not advance")
	}
	click(t, tt, "播放或暂停 GIF")
	tt.Frame()
	paused := tt.Image().RGBAAt(x, y)
	tt.Advance(400 * time.Millisecond)
	if tt.Image().RGBAAt(x, y) != paused {
		t.Fatal("paused GIF kept advancing")
	}
	a.previewOpen = false
	tt.Frame()
	if a.previewAnimation != nil {
		t.Fatal("GIF frames retained after closing")
	}
}

func TestSVGPreviewPreservesOriginalDimensions(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="red"/></svg>`)
	b.mu.Lock()
	b.images = map[string][]byte{"vector": svg}
	b.mu.Unlock()
	a.openArtwork(domain.ImageRecord{ID: "vector", Prompt: "矢量图", MIMEType: "image/svg+xml", Width: 40, Height: 20})
	settle(a, tt)
	if a.previewBitmap == nil || a.previewWidth != 40 || a.previewHeight != 20 || a.problem != "" {
		t.Fatalf("SVG preview failed: %s", a.problem)
	}
}

func TestFailedTaskKeepsCompleteErrorReadableWithoutChangingRequest(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	message := "生图请求失败（HTTP 400）。参数 output_format。渠道提示：Unsupported output_format: gif. Supported values are png, jpeg, webp."
	task := &job{Args: domain.GenerateArgs{JobID: "format-failure", Prompt: "原有提示词", Model: "fixture-image", OutputFormat: "gif"}, Progress: domain.Progress{Status: "error", Stage: "生成失败", Error: message}}
	a.jobs = []*job{task}
	tt.Frame()
	details, detailsOK := tt.Find("生成失败原因")
	retry, retryOK := tt.Find("重试")
	if !detailsOK {
		t.Fatal("complete error details are not accessible")
	}
	if !retryOK || details.Y+details.H > retry.Y {
		t.Fatal("error details overlap retry action")
	}
	click(t, tt, "生成失败原因")
	tt.Type("不能修改错误")
	tt.Frame()
	if task.Progress.Error != message || task.Args.OutputFormat != "gif" || task.Args.Prompt != "原有提示词" {
		t.Fatal("diagnostic view changed the error or request")
	}
	if _, ok := tt.Find("重试"); !ok {
		t.Fatal("retry must remain available")
	}
}

func TestGIFRateAndFrameControlsPreserveDraftAndRequest(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}} {
		a, tt, b, _ := fixture(t, size[0], size[1])
		a.prompt = "给我生成一个6*6格子的搞笑头像，每个格式一个不同动画。"
		ratio, resolution := a.ratio, a.resolution
		click(t, tt, "输出格式")
		click(t, tt, "GIF")
		tt.Frame()
		for _, label := range []string{"GIF 单帧尺寸", "GIF 帧数", "GIF 帧率", "开始生成"} {
			r, ok := tt.Find(label)
			if !ok || r.X < 0 || r.X+r.W > float32(size[0])+1 || r.Y+r.H > float32(size[1])+1 {
				t.Fatalf("GIF control outside %v: %s %+v", size, label, r)
			}
		}
		click(t, tt, "GIF 帧率")
		click(t, tt, "10 帧/秒")
		settle(a, tt)
		click(t, tt, "GIF 帧数")
		click(t, tt, "12 帧")
		settle(a, tt)
		if b.config.GenerationPreferences.GIFFrameDurationMS != 100 || b.config.GenerationPreferences.GIFFrameCount != 12 {
			t.Fatal("GIF timing not saved")
		}
		a.openSettings()
		tt.Frame()
		a.settingsOpen = false
		tt.Frame()
		b.generateStarted = make(chan domain.GenerateArgs, 1)
		prompt := a.prompt
		a.generate()
		args := <-b.generateStarted
		settle(a, tt)
		if args.FrameDurationMS != 100 || args.FrameCount != 12 || args.Size != "1024x1024" || args.Prompt != prompt || a.prompt != "" {
			t.Fatal("GIF timing or prompt lost")
		}
		click(t, tt, "输出格式")
		click(t, tt, "PNG")
		tt.Frame()
		if a.ratio != ratio || a.resolution != resolution {
			t.Fatal("static image parameters changed")
		}
		if _, visible := tt.Find("GIF 帧率"); visible {
			t.Fatal("animation controls remain in static image mode")
		}
	}
}
