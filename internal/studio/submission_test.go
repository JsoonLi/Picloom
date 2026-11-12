package studio

import (
	"errors"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

func TestSubmitClearsSubmittedPromptAndReferences(t *testing.T) {
	for _, keyboard := range []bool{false, true} {
		t.Run(map[bool]string{false: "button", true: "shortcut"}[keyboard], func(t *testing.T) {
			a, tt, b, _ := fixture(t, 1280, 900)
			a.prompt = "已提交的提示词"
			submittedImage := generation.DataURL(b.image, "image/png")
			a.references = []domain.Reference{{ID: "ref", Name: "已提交图片.png", DataURL: submittedImage}}
			thumb, err := ui.DecodeBitmap(b.image)
			if err != nil {
				t.Fatal(err)
			}
			a.referenceThumbs["ref"] = thumb
			b.generateStarted = make(chan domain.GenerateArgs, 1)
			b.generateRelease = make(chan struct{})
			defer close(b.generateRelease)
			tt.Frame()
			if _, ok := tt.Find("标注编辑参考图 已提交图片.png"); !ok {
				t.Fatal("reference thumbnail must be visible before submission")
			}
			if keyboard {
				click(t, tt, "提示词")
				tt.Key(ui.Cmd, ui.KeyEnter)
			} else {
				click(t, tt, "开始生成")
			}
			args := <-b.generateStarted
			if a.prompt != "" {
				t.Fatalf("submitted prompt still in composer: %q", a.prompt)
			}
			if args.Prompt != "已提交的提示词" || len(args.ReferenceImages) != 1 || args.ReferenceImages[0] != submittedImage || len(a.jobs) != 1 {
				t.Fatal("submission lost the original request or references")
			}
			if len(a.references) != 0 || len(a.referenceThumbs) != 0 {
				t.Fatal("submitted references and thumbnails remain in composer")
			}
			tt.Frame()
			if _, ok := tt.Find("标注编辑参考图 已提交图片.png"); ok || !tt.HasText(fmt.Sprintf("参考图 0/%d", domain.MaxReferenceImages)) {
				t.Fatal("submitted reference remains visible in the composer")
			}
			click(t, tt, "提示词")
			tt.Type("下一条创作草稿")
			if a.prompt != "下一条创作草稿" {
				t.Fatalf("prompt editor retained submitted text: %q", a.prompt)
			}
			a.references = []domain.Reference{{ID: "next", Name: "下一条图片.png", DataURL: submittedImage}}
			a.referenceThumbs["next"] = thumb
			a.openSettings()
			tt.Frame()
			a.settingsOpen = false
			tt.Frame()
			b.mu.Lock()
			if keyboard {
				b.fail = errors.New("模拟失败")
			}
			b.mu.Unlock()
			b.generateRelease <- struct{}{}
			settle(a, tt)
			if a.prompt != "下一条创作草稿" || len(a.references) != 1 || a.references[0].ID != "next" || a.referenceThumbs["next"] == nil {
				t.Fatal("completion or settings changed the next draft")
			}
			if keyboard {
				if len(a.jobs) != 1 || a.jobs[0].Args.Prompt != args.Prompt || len(a.jobs[0].Args.ReferenceImages) != 1 || a.jobs[0].Args.ReferenceImages[0] != submittedImage {
					t.Fatal("failure lost the retry prompt or images")
				}
				b.mu.Lock()
				b.fail = nil
				b.mu.Unlock()
				click(t, tt, "重做")
				settle(a, tt)
				if len(b.generateStarted) != 0 || a.prompt != args.Prompt || len(a.references) != 1 || a.references[0].DataURL != submittedImage {
					t.Fatal("redo must restore the failed request without sending it")
				}
				a.prompt = "修改后再发送"
				click(t, tt, "开始生成")
				redone := <-b.generateStarted
				if redone.Prompt != "修改后再发送" || len(redone.ReferenceImages) != 1 || redone.ReferenceImages[0] != submittedImage {
					t.Fatal("edited redo lost submitted images")
				}
				b.generateRelease <- struct{}{}
				settle(a, tt)
				if a.prompt != "" || len(a.references) != 0 {
					t.Fatal("explicit redo submission did not clear submitted input")
				}
			} else if len(a.history.Items) != 1 || a.history.Items[0].Prompt != args.Prompt {
				t.Fatal("success lost the submitted prompt")
			}
		})
	}
}
