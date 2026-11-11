package studio

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

func openMarkupFixture(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	request := a.beginPreview("山间小屋.png", nil, nil, 0, 0)
	a.loadPreview(request, func() ([]byte, error) { return visualArtwork(0), nil }, nil)
	settle(a, tt)
	click(t, tt, "标注编辑当前图片")
	settle(a, tt)
	if a.markup == nil || a.markup.picture == nil {
		t.Fatal("editor did not load the source")
	}
}

func TestReferenceThumbnailOpensMarkupDirectly(t *testing.T) {
	a, tt, _, desktop := fixture(t, 1280, 900)
	a.prompt = "保留工作台草稿"
	desktop.refs = []domain.Reference{{ID: "direct-markup", Name: "直接标注.png", DataURL: generation.DataURL(visualArtwork(0), "image/png")}}
	click(t, tt, "添加参考图")
	settle(a, tt)
	box, ok := tt.Find("标注编辑参考图 直接标注.png")
	if !ok {
		t.Fatal("reference thumbnail missing")
	}
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	if a.markup == nil {
		t.Fatal("reference click must open markup immediately, including while loading")
	}
	settle(a, tt)
	if _, ok := tt.Find("图片标注画布"); !ok {
		t.Fatal("reference click did not reach the annotation canvas")
	}
	if _, ok := tt.Find("图片缩放查看器"); ok {
		t.Fatal("reference click must skip the intermediate preview")
	}
	if output := os.Getenv("IMAGE2AI_MARKUP_SNAPSHOTS"); output != "" {
		writeMarkupSnapshot(t, tt, output, "reference-direct-entry")
	}
	click(t, tt, "取消标注")
	if a.previewOpen || len(a.references) != 1 || a.prompt != "保留工作台草稿" {
		t.Fatal("cancel must return to the composer and keep the reference")
	}
}

func TestDirectMarkupReadFailureCanClose(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	a.openReference(domain.Reference{Name: "无效图片", DataURL: "data:image/png;base64,YmFk"})
	settle(a, tt)
	if a.markup == nil || a.markup.problem == "" || a.markup.busy {
		t.Fatal("failed source must report an error in the editor")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.previewOpen || a.markup != nil || a.previewLoading {
		t.Fatal("closing failed direct editor must release its state")
	}
}

func drawMarkupRectangle(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	click(t, tt, "标注工具：矩形")
	box, ok := tt.Find("图片标注画布")
	if !ok {
		t.Fatal("canvas missing")
	}
	r := a.markup.target(box.W, box.H)
	x, y := box.X+r.X, box.Y+r.Y
	tt.Press(x+r.W*0.35, y+r.H*0.43)
	tt.Move(x+r.W*0.6, y+r.H*0.76)
	tt.Release(x+r.W*0.6, y+r.H*0.76)
	tt.Frame()
	if len(a.markup.strokes) != 1 {
		t.Fatal("pointer gesture did not create a rectangle")
	}
}

func TestMarkupLayoutAndPointerTools(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}, {320, 768}} {
		for _, dark := range []bool{false, true} {
			theme := map[bool]string{false: "light", true: "dark"}[dark]
			t.Run(fmtSize(size)+"-"+theme, func(t *testing.T) {
				a, tt, _, _ := fixture(t, size[0], size[1])
				tt.SetDark(dark)
				openMarkupFixture(t, a, tt)
				drawMarkupRectangle(t, a, tt)
				for _, label := range []string{"图片标注画布", "标注工具：画笔", "标注工具：移动", "撤销标注", "标注线宽"} {
					box, ok := tt.Find(label)
					if !ok || box.X < 0 || box.Y < 0 || box.X+box.W > float32(size[0]) || box.Y+box.H > float32(size[1]) {
						t.Fatalf("%s outside window: %v", label, box)
					}
				}
				if output := os.Getenv("IMAGE2AI_MARKUP_SNAPSHOTS"); output != "" {
					writeMarkupSnapshot(t, tt, output, fmtSize(size)+"-"+theme+"-canvas")
				}
				click(t, tt, "撤销标注")
				if len(a.markup.strokes) != 0 {
					t.Fatal("undo did not remove stroke")
				}
				click(t, tt, "重做标注")
				if len(a.markup.strokes) != 1 {
					t.Fatal("redo did not restore stroke")
				}
				if size[0] < 900 {
					box, _ := tt.Find("图片标注画布")
					tt.Scroll(box.X+5, box.Y+box.H+40, 0, 2000)
					tt.Frame()
				}
				for _, label := range []string{"标注编辑模型", "标注编辑画面比例", "标注编辑分辨率", "标注编辑输出格式", "生成标注编辑", "取消标注"} {
					box, ok := tt.Find(label)
					if !ok || box.X < 0 || box.X+box.W > float32(size[0]) || box.Y < 0 || box.Y+box.H > float32(size[1]) {
						t.Fatalf("form action %s unreachable: %v", label, box)
					}
				}
				action, _ := tt.Find("生成标注编辑")
				for _, label := range []string{"使用 GPT Image 2.6\n请确认渠道支持标注编辑。", "生成结果另存作品库。"} {
					box, ok := tt.Find(label)
					if !ok || box.X+box.W > action.X+action.W+1 {
						t.Fatalf("helper text must wrap within the form: %s %v", label, box)
					}
					if size[0] == 320 && label == "使用 GPT Image 2.6\n请确认渠道支持标注编辑。" && box.H < 24 {
						t.Fatalf("narrow helper must occupy two readable lines: %s %v", label, box)
					}
				}
				if output := os.Getenv("IMAGE2AI_MARKUP_SNAPSHOTS"); output != "" {
					writeMarkupSnapshot(t, tt, output, fmtSize(size)+"-"+theme+"-instructions")
				}
				click(t, tt, "取消标注")
				if a.markup != nil || !a.previewOpen {
					t.Fatal("cancel must return to the image preview")
				}
			})
		}
	}
}

func TestMarkupOutputSizeChoicesReachRequestWithoutResizingAnnotations(t *testing.T) {
	for _, width := range []int{1280, 375} {
		t.Run(fmtSize([2]int{width, 900}), func(t *testing.T) {
			a, tt, b, _ := fixture(t, width, 900)
			a.config.Providers[0].DefaultResolution = "4k"
			openMarkupFixture(t, a, tt)
			drawMarkupRectangle(t, a, tt)
			e := a.markup
			e.prompt = "保持标注位置，修改小屋"
			point := e.strokes[0].Points[0]
			if width < 900 {
				box, _ := tt.Find("图片标注画布")
				tt.Scroll(box.X+5, box.Y+box.H+40, 0, 2000)
				tt.Frame()
			}
			click(t, tt, "标注编辑画面比例")
			click(t, tt, "16:9 横版")
			if e.resolution != "4k" {
				t.Fatal("manual ratio must start with the selected provider's default resolution")
			}
			click(t, tt, "标注编辑分辨率")
			click(t, tt, "自动")
			if size, _ := e.requestSize(); size != "auto" || e.ratio != "auto" {
				t.Fatal("automatic output must not retain an ignored manual ratio")
			}
			click(t, tt, "标注编辑分辨率")
			click(t, tt, "4K")
			if size, _ := e.requestSize(); e.ratio != "source" || size != "4736x3552" {
				t.Fatal("resolution selection must preserve the source aspect ratio")
			}
			click(t, tt, "标注编辑画面比例")
			click(t, tt, "16:9 横版")
			click(t, tt, "标注编辑分辨率")
			tt.Key(0, ui.KeyHome)
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyEnter)
			if !tt.HasText("输出 2736 × 1536 px") {
				t.Fatalf("output pixel dimensions must follow both selectors: ratio=%s resolution=%s", e.ratio, e.resolution)
			}
			if e.size != image.Pt(640, 480) || e.strokes[0].Points[0] != point || a.ratio != "3:4" || a.resolution != "2k" {
				t.Fatal("output controls changed annotation coordinates or studio options")
			}
			if output := os.Getenv("IMAGE2AI_MARKUP_SNAPSHOTS"); output != "" {
				writeMarkupSnapshot(t, tt, output, "selected-output-"+fmtSize([2]int{width, 900}))
			}
			b.generateStarted = make(chan domain.GenerateArgs, 1)
			click(t, tt, "生成标注编辑")
			settle(a, tt)
			args := <-b.generateStarted
			if args.Size != "2736x1536" {
				t.Fatalf("output size not submitted: %s", args.Size)
			}
			for _, encoded := range []string{args.ReferenceImages[0], args.Markup} {
				data, _, err := generation.ValidateReference(encoded)
				if err != nil {
					t.Fatal(err)
				}
				size, err := generation.ImageSize(data)
				if err != nil || size != image.Pt(640, 480) {
					t.Fatal("source and overlay must retain their original pixel dimensions")
				}
			}
		})
	}
}

func TestMarkupResolutionUsesEditingProvider(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	a.config.Providers = append(a.config.Providers, domain.Provider{ID: "other", Name: "只支持2K", Models: []string{"edit-image"}, Resolutions: []string{"2k"}, DefaultResolution: "2k"})
	openMarkupFixture(t, a, tt)
	click(t, tt, "标注编辑分辨率")
	click(t, tt, "4K")
	click(t, tt, "标注编辑模型")
	click(t, tt, "只支持2K / edit-image")
	if a.markup.providerID != "other" || a.markup.resolution != "2k" || a.providerID != "local" {
		t.Fatal("editing provider must reconcile its own resolution without changing the studio")
	}
	click(t, tt, "标注编辑分辨率")
	if tt.HasText("4K") {
		t.Fatal("unsupported resolution is offered by the edit selector")
	}
	tt.Key(0, ui.KeyEscape)
}

func TestMarkupOutputSizeValidation(t *testing.T) {
	for _, tc := range []struct {
		ratio, resolution, want string
		source                  image.Point
	}{
		{"auto", "auto", "auto", image.Pt(640, 480)},
		{"source", "2k", "2368x1776", image.Pt(640, 480)},
		{"1:1", "4k", "4096x4096", image.Pt(640, 480)},
		{"16:9", "2k", "2736x1536", image.Pt(640, 480)},
		{"source", "4k", "", image.Pt(32768, 1)},
		{"source", "2k", "", image.Point{}},
		{"1:1", "8k", "", image.Pt(640, 480)},
		{"invalid", "2k", "", image.Pt(640, 480)},
	} {
		e := markupEditor{ratio: tc.ratio, resolution: tc.resolution, size: tc.source}
		size, err := e.requestSize()
		if (err != nil) != (tc.want == "") || size != tc.want {
			t.Fatalf("%s %s %v: got %q, %v", tc.ratio, tc.resolution, tc.source, size, err)
		}
	}
}

func writeMarkupSnapshot(t *testing.T, tt *ui.Tester, output, name string) {
	t.Helper()
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(output, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestMarkupZoomPanAndSubmissionPreserveSourceAndStudioDraft(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	a.prompt = "工作台未提交的草稿"
	a.references = []domain.Reference{{ID: "keep", Name: "保留参考图", DataURL: generation.DataURL(b.image, "image/png")}}
	a.outputFormat = "gif"
	openMarkupFixture(t, a, tt)
	if a.markup.outputFormat != "png" || a.outputFormat != "gif" {
		t.Fatal("editor must offer still formats independently of the studio")
	}
	drawMarkupRectangle(t, a, tt)
	click(t, tt, "标注画布放大")
	box, _ := tt.Find("图片标注画布")
	click(t, tt, "标注工具：移动")
	tt.Press(box.X+box.W/2, box.Y+box.H/2)
	tt.Move(box.X+box.W/2+32, box.Y+box.H/2-24)
	tt.Release(box.X+box.W/2+32, box.Y+box.H/2-24)
	click(t, tt, "标注工具：画笔")
	r := a.markup.target(box.W, box.H)
	x, y := box.X+r.X+r.W*0.25, box.Y+r.Y+r.H*0.5
	tt.Press(x, y)
	tt.Move(x+10, y+10)
	tt.Release(x+10, y+10)
	e := a.markup
	if len(e.strokes) != 2 || math.Abs(float64(e.strokes[1].Points[0][0]-160)) > 0.1 || math.Abs(float64(e.strokes[1].Points[0][1]-240)) > 0.1 {
		t.Fatal("zoomed/panned annotation lost its source coordinates")
	}
	click(t, tt, "标注修改要求")
	tt.Type("把标注的小屋改成木屋")
	b.generateStarted = make(chan domain.GenerateArgs, 2)
	b.mu.Lock()
	b.fail = errors.New("本机模拟渠道暂时失败")
	b.mu.Unlock()
	click(t, tt, "生成标注编辑")
	settle(a, tt)
	args := <-b.generateStarted
	if args.Markup == "" || len(args.ReferenceImages) != 1 || args.ReferenceImages[0] != e.source || args.Size != "auto" || args.Prompt != "把标注的小屋改成木屋" {
		t.Fatal("edit request lost its source, overlay or instruction")
	}
	original, _, _ := generation.ValidateReference(args.ReferenceImages[0])
	if !bytes.Equal(original, visualArtwork(0)) {
		t.Fatal("marks were burned into the source image")
	}
	if a.prompt != "工作台未提交的草稿" || len(a.references) != 1 || a.references[0].ID != "keep" || a.previewOpen {
		t.Fatal("edit submission altered the studio draft")
	}
	b.mu.Lock()
	b.fail = nil
	b.mu.Unlock()
	click(t, tt, "重做")
	settle(a, tt)
	if len(b.generateStarted) != 0 || a.draftMarkup != args.Markup || a.requestSize() != args.Size || a.prompt != args.Prompt {
		t.Fatal("redo must restore markup and automatic size without sending")
	}
	click(t, tt, "标注编辑参考图 参考图 1")
	settle(a, tt)
	if a.markup == nil || a.markup.baseMarkup != args.Markup || a.markup.baseBitmap == nil || a.markup.prompt != args.Prompt {
		t.Fatal("restored annotations must remain visible and editable")
	}
	click(t, tt, "取消标注")
	a.prompt = "重做修改后的说明"
	click(t, tt, "开始生成")
	settle(a, tt)
	redone := <-b.generateStarted
	if redone.Markup != args.Markup || redone.ReferenceImages[0] != args.ReferenceImages[0] || redone.Prompt != "重做修改后的说明" {
		t.Fatal("redo lost markup or edited instruction")
	}
}

func TestMarkupCancelledPreparationCannotSubmit(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	openMarkupFixture(t, a, tt)
	drawMarkupRectangle(t, a, tt)
	a.markup.prompt = "取消这次请求"
	b.generateStarted = make(chan domain.GenerateArgs, 1)
	a.submitMarkup(a.markup)
	a.previewOpen = false
	settle(a, tt)
	if len(b.generateStarted) != 0 {
		t.Fatal("closing editor during preparation submitted a request")
	}
	if a.markup != nil {
		t.Fatal("closed editor retained full-resolution data")
	}
}
