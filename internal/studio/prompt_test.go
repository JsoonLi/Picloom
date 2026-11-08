package studio

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

func TestPromptCleansBlankLinesWithoutInterruptingTyping(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	click(t, tt, "提示词")
	tt.SetClipboard("\r\n生成一只橘猫。\r\n\r\n \t\r\n角色：圆滚滚的身体。")
	tt.Key(ui.Cmd, ui.KeyV)
	if a.prompt != "生成一只橘猫。\n角色：圆滚滚的身体。" {
		t.Fatalf("blank lines remain after paste: %q", a.prompt)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeyEnter)
	if !strings.HasSuffix(a.prompt, "身体。\n") {
		t.Fatalf("normal newline was removed or repeated: %q", a.prompt)
	}
	before := a.prompt
	tt.Compose("biaoqing", 8)
	tt.Compose("表情", 2)
	if a.prompt != before {
		t.Fatal("cleanup committed IME preedit")
	}
	tt.Type("表情：开心。")
	if a.prompt != before+"表情：开心。" {
		t.Fatalf("typing after cleanup lost its position: %q", a.prompt)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.SetClipboard("\n \t\n\n")
	tt.Key(ui.Cmd, ui.KeyV)
	if a.prompt != "" || a.canGenerate() {
		t.Fatalf("whitespace-only input was not cleared: %q", a.prompt)
	}
}

func TestPromptReuseCleansBlankLines(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	a.reuse(domain.ImageRecord{ID: "old-prompt", Prompt: "\n橘猫\n\n  动作：眨眼。", ProviderID: "local", Model: "studio-image", Size: "1024x1024"})
	settle(a, tt)
	if a.prompt != "橘猫\n  动作：眨眼。" {
		t.Fatalf("reused prompt kept blank lines or lost indentation: %q", a.prompt)
	}
}

func TestPromptCleanupPreservesMiddlePasteCaret(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	click(t, tt, "提示词")
	tt.Type("前文后文")
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	tt.SetClipboard("\n\n角色\n\n")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Type("新")
	if a.prompt != "前文\n角色\n新后文" {
		t.Fatalf("cleanup moved the insertion point: %q", a.prompt)
	}
}

func TestPromptGrowsWithWrappedTextAndShrinksAfterClearing(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-dark-%t", fmtSize(size), dark), func(t *testing.T) {
				a, tt, _, _ := fixture(t, size[0], size[1])
				tt.SetDark(dark)
				a.outputFormat = "png"
				tt.Frame()
				empty, _ := tt.Find("提示词")
				click(t, tt, "提示词")
				tt.Type(strings.Repeat("橘猫在镜头前眨眼，画面保持一致。", 30))
				tt.Frame()
				grown, _ := tt.Find("提示词")
				generate, _ := tt.Find("开始生成")
				emptyLibrary, _ := tt.Find("作品库空状态")
				if emptyLibrary.Y+emptyLibrary.H >= grown.Y {
					t.Fatalf("growing prompt overlaps library: library=%+v prompt=%+v", emptyLibrary, grown)
				}
				if grown.H <= empty.H || grown.H > float32(size[1])*0.4 || generate.Y+generate.H > float32(size[1]) {
					t.Fatalf("input must grow within the window: empty=%+v grown=%+v generate=%+v", empty, grown, generate)
				}
				t.Logf("%v: prompt height %.1f -> %.1f", size, empty.H, grown.H)
				if output := os.Getenv("IMAGE2AI_PROMPT_SNAPSHOTS"); output != "" {
					if err := os.MkdirAll(output, 0700); err != nil {
						t.Fatal(err)
					}
					file, err := os.Create(filepath.Join(output, fmt.Sprintf("prompt-%s-dark-%t.png", fmtSize(size), dark)))
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, tt.Image())
					file.Close()
					if err != nil {
						t.Fatal(err)
					}
				}
				a.clearInput()
				tt.Frame()
				cleared, _ := tt.Find("提示词")
				if cleared.H != empty.H {
					t.Fatalf("cleared prompt did not shrink: %+v", cleared)
				}
			})
		}
	}
}
