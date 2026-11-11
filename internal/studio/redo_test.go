package studio

import (
	"fmt"
	"image"
	"os"
	"testing"

	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

func TestFailedJobRedoRestoresDraftWithoutSending(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	image := generation.DataURL(b.image, "image/png")
	args := domain.GenerateArgs{JobID: "failed", Prompt: "重新为插画上色", ProviderID: "local", Model: "studio-image", Size: "3552x4736", OutputFormat: "webp", ReferenceImages: []string{image}}
	a.jobs = []*job{{Args: args, Progress: domain.Progress{Status: "error", Stage: "生成失败", Error: "本机模拟错误"}}}
	b.generateStarted = make(chan domain.GenerateArgs, 1)
	tt.Frame()
	if output := os.Getenv("IMAGE2AI_REDO_SNAPSHOTS"); output != "" {
		writeMarkupSnapshot(t, tt, output, "failed-card-light")
		tt.SetDark(true)
		writeMarkupSnapshot(t, tt, output, "failed-card-dark")
	}
	click(t, tt, "重做")
	settle(a, tt)
	if len(b.generateStarted) != 0 {
		t.Fatal("redo must restore editable input without sending a request")
	}
	if a.prompt != args.Prompt || len(a.references) != 1 || a.references[0].DataURL != image || a.referenceThumbs[a.references[0].ID] == nil {
		t.Fatal("redo lost prompt or reference images")
	}
	if a.providerID != args.ProviderID || a.model != args.Model || a.ratio != "3:4" || a.resolution != "4k" || a.outputFormat != "webp" || len(a.jobs) != 0 {
		t.Fatal("redo lost generation options or did not remove the old task")
	}
	if output := os.Getenv("IMAGE2AI_REDO_SNAPSHOTS"); output != "" {
		writeMarkupSnapshot(t, tt, output, "restored-draft")
		tt.SetDark(false)
		writeMarkupSnapshot(t, tt, output, "restored-draft-light")
	}
	a.prompt = "改成水彩配色"
	click(t, tt, "开始生成")
	settle(a, tt)
	sent := <-b.generateStarted
	if sent.Prompt != a.history.Items[0].Prompt || sent.Prompt != "改成水彩配色" || sent.Size != args.Size || sent.OutputFormat != args.OutputFormat || len(sent.ReferenceImages) != 1 || sent.ReferenceImages[0] != image {
		t.Fatal("explicit submission did not use the edited draft")
	}
}

func TestDraftRestoreCannotReplaceInputAfterClear(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	started, release := make(chan struct{}), make(chan struct{})
	gate := release
	defer func() {
		if release != nil {
			close(release)
		}
		a.workers.Wait()
	}()
	completed := false
	a.restoreGenerationDraft(domain.GenerateArgs{Prompt: "旧任务", Size: "2048x2048"}, func() ([]domain.Reference, error) {
		close(started)
		<-gate
		return []domain.Reference{{ID: "old", DataURL: generation.DataURL(b.image, "image/png")}}, nil
	}, func() { completed = true })
	<-started
	a.clearInput()
	a.prompt = "后输入的新草稿"
	close(release)
	release = nil
	settle(a, tt)
	if completed || a.importing || a.prompt != "后输入的新草稿" || len(a.references) != 0 {
		t.Fatal("stale restoration replaced a newer draft")
	}
}

func TestRemovingRestoredMarkupSourceClearsOverlayAndAutomaticSize(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	a.references = []domain.Reference{{ID: "marked"}}
	a.draftMarkup, a.draftSize = "old-markup", "auto"
	a.removeReference("marked")
	tt.Frame()
	if a.draftMarkup != "" || a.draftSize != "" || a.requestSize() == "auto" {
		t.Fatal("removing the source must clear its annotation and automatic-edit-only size")
	}
}

func TestFailedJobRedoRestoresGIFOptions(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	args := domain.GenerateArgs{JobID: "gif-redo", Prompt: "修改动图动作", ProviderID: "local", Model: "studio-image", Size: "512x512", OutputFormat: "gif", FrameCount: 12, FrameDurationMS: 80}
	a.jobs = []*job{{Args: args, Progress: domain.Progress{Status: "error", Stage: "生成失败"}}}
	b.generateStarted = make(chan domain.GenerateArgs, 1)
	tt.Frame()
	click(t, tt, "重做")
	settle(a, tt)
	if len(b.generateStarted) != 0 || a.outputFormat != "gif" || a.gifSize != args.Size || a.gifFrameCount != 12 || a.gifFrameDurationMS != 80 {
		t.Fatal("redo lost GIF options or sent automatically")
	}
	click(t, tt, "开始生成")
	settle(a, tt)
	sent := <-b.generateStarted
	if sent.Size != args.Size || sent.OutputFormat != args.OutputFormat || sent.FrameCount != 12 || sent.FrameDurationMS != 80 {
		t.Fatal("explicit GIF redo lost its options")
	}
}

func TestFailedJobRedoInvalidReferenceKeepsDraftAndTask(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	a.prompt = "保留当前草稿"
	a.references = []domain.Reference{{ID: "keep", DataURL: generation.DataURL(b.image, "image/png")}}
	task := &job{Args: domain.GenerateArgs{JobID: "bad-redo", Prompt: "无法恢复的任务", ReferenceImages: []string{"invalid image data"}}, Progress: domain.Progress{Status: "error", Stage: "生成失败"}}
	a.jobs = []*job{task}
	b.generateStarted = make(chan domain.GenerateArgs, 1)
	tt.Frame()
	click(t, tt, "重做")
	settle(a, tt)
	if len(b.generateStarted) != 0 || a.prompt != "保留当前草稿" || len(a.references) != 1 || a.references[0].ID != "keep" || len(a.jobs) != 1 || a.jobs[0] != task || a.problem == "" || a.importing {
		t.Fatal("failed restoration must retain the current draft and failed task")
	}
}

func TestFailedJobRedoPreservesOriginalSizeUntilEdited(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	a.jobs = []*job{{Args: domain.GenerateArgs{JobID: "size-redo", Prompt: "原尺寸任务", ProviderID: "local", Model: "studio-image", Size: "1024x1024"}, Progress: domain.Progress{Status: "error", Stage: "生成失败"}}}
	b.generateStarted = make(chan domain.GenerateArgs, 1)
	tt.Frame()
	click(t, tt, "重做")
	settle(a, tt)
	if a.requestSize() != "1024x1024" || !tt.HasText("沿用") || len(b.generateStarted) != 0 {
		t.Fatal("redo changed the original output size")
	}
	click(t, tt, "画面比例")
	click(t, tt, "16:9 横版")
	if a.draftSize != "" || a.requestSize() != "2736x1536" {
		t.Fatal("manual size selection did not replace the original size")
	}
}

func TestRedoMarkedDraftFitsNarrowWindow(t *testing.T) {
	for _, width := range []int{320, 375} {
		a, tt, b, _ := fixture(t, width, 900)
		data, err := generation.RenderMarkupPNG(image.Pt(48, 32), []generation.MarkupStroke{{Tool: "pen", Points: [][2]float32{{2, 2}, {20, 10}}, Width: 2}})
		if err != nil {
			t.Fatal(err)
		}
		args := domain.GenerateArgs{JobID: "marked-redo", Prompt: "修改圈内部分的配色", ProviderID: "local", Model: "studio-image", Size: "auto", OutputFormat: "png", Markup: generation.DataURL(data, "image/png"), ReferenceImages: []string{generation.DataURL(b.image, "image/png")}}
		task := &job{Args: args, Progress: domain.Progress{Status: "error"}}
		a.jobs = []*job{task}
		a.redoJob(task)
		settle(a, tt)
		for _, dark := range []bool{false, true} {
			tt.SetDark(dark)
			for _, label := range []string{"画面比例", "分辨率", "输出格式", "模型", "开始生成", "标注编辑参考图 参考图 1"} {
				box, ok := tt.Find(label)
				if !ok || box.X < 0 || box.Y < 0 || box.X+box.W > float32(width) || box.Y+box.H > 900 {
					t.Fatalf("restored %s overflows %d: %v", label, width, box)
				}
			}
			if !tt.HasText("参考图 1/1") || !tt.HasText("含标注") || !tt.HasText("沿用") || a.requestSize() != "auto" {
				t.Fatal("restored markup and size state must be visible")
			}
			if output := os.Getenv("IMAGE2AI_REDO_SNAPSHOTS"); output != "" {
				writeMarkupSnapshot(t, tt, output, fmt.Sprintf("restored-markup-%d-%v", width, dark))
			}
		}
	}
}

func TestRestoredMarkupCanAppendWithoutBurningSource(t *testing.T) {
	size := image.Pt(48, 32)
	oldStroke := generation.MarkupStroke{Tool: "pen", Points: [][2]float32{{4, 4}, {20, 4}}, Width: 2}
	data, err := generation.RenderMarkupPNG(size, []generation.MarkupStroke{oldStroke})
	if err != nil {
		t.Fatal(err)
	}
	base := generation.DataURL(data, "image/png")
	unchanged, err := renderMarkupOverlay(size, nil, base)
	if err != nil || unchanged != base {
		t.Fatal("unchanged annotations must retain the original layer")
	}
	newStroke := generation.MarkupStroke{Tool: "pen", Points: [][2]float32{{4, 20}, {20, 20}}, Width: 2}
	merged, err := renderMarkupOverlay(size, []generation.MarkupStroke{newStroke}, base)
	if err != nil {
		t.Fatal(err)
	}
	img, err := readMarkupPNG(merged, size)
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range []image.Point{{10, 4}, {10, 20}} {
		if _, _, _, alpha := img.At(point.X, point.Y).RGBA(); alpha == 0 {
			t.Fatal("merged overlay lost old or new annotations")
		}
	}
	if _, _, _, alpha := img.At(30, 30).RGBA(); alpha != 0 {
		t.Fatal("overlay background must remain transparent")
	}
}

func markedRedoRequest(t *testing.T, source []byte) domain.GenerateArgs {
	t.Helper()
	data, err := generation.RenderMarkupPNG(image.Pt(48, 32), []generation.MarkupStroke{{Tool: "pen", Points: [][2]float32{{2, 2}, {20, 10}}, Width: 2}})
	if err != nil {
		t.Fatal(err)
	}
	return domain.GenerateArgs{JobID: "marked-exact", Prompt: "保留尺寸追加标注", ProviderID: "local", Model: "studio-image", Size: "1024x1024", OutputFormat: "png", Markup: generation.DataURL(data, "image/png"), ReferenceImages: []string{generation.DataURL(source, "image/png")}}
}

func TestRestoredMarkupEditorPreservesExactSizeUntilChanged(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprint(manual), func(t *testing.T) {
			a, tt, b, _ := fixture(t, 1280, 900)
			args := markedRedoRequest(t, b.image)
			a.redoJob(&job{Args: args, Progress: domain.Progress{Status: "error"}})
			settle(a, tt)
			a.openReference(a.references[0])
			settle(a, tt)
			size, err := a.markup.requestSize()
			if err != nil || size != args.Size {
				t.Fatalf("reopened markup changed exact size: got %q, want %q: %v", size, args.Size, err)
			}
			if manual {
				click(t, tt, "标注编辑画面比例")
				click(t, tt, "16:9 横版")
				if size, err := a.markup.requestSize(); err != nil || size != "2736x1536" {
					t.Fatalf("manual dimensions did not replace exact size: %q: %v", size, err)
				}
			} else {
				b.generateStarted = make(chan domain.GenerateArgs, 1)
				click(t, tt, "生成标注编辑")
				settle(a, tt)
				sent := <-b.generateStarted
				if sent.Size != args.Size || sent.Markup != args.Markup {
					t.Fatal("submission lost exact size or original annotations")
				}
			}
		})
	}
}

func TestMarkedRedoRejectsUnsupportedOutputFormats(t *testing.T) {
	for _, menu := range []bool{false, true} {
		t.Run(fmt.Sprint(menu), func(t *testing.T) {
			a, tt, b, _ := fixture(t, 1280, 900)
			a.redoJob(&job{Args: markedRedoRequest(t, b.image), Progress: domain.Progress{Status: "error"}})
			settle(a, tt)
			if menu {
				click(t, tt, "输出格式")
				if tt.HasText("SVG") || tt.HasText("GIF") {
					t.Fatal("marked draft offers unsupported output formats")
				}
			} else {
				for _, format := range []string{"svg", "gif"} {
					a.outputFormat = format
					tt.Frame()
					if a.canGenerate() {
						t.Fatalf("marked %s draft can be submitted", format)
					}
				}
			}
		})
	}
}
