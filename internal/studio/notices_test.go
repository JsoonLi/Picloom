package studio

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"image2ai/internal/domain"
)

func TestAppearanceSaveDoesNotShowNotice(t *testing.T) {
	a, tt, b, d := fixture(t, 1280, 900)
	a.openSettings()
	a.settingsTab = "外观"
	tt.Frame()
	click(t, tt, "选择主题")
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyEnter)
	settle(a, tt)
	if b.config.Theme != "dark" || d.theme != "dark" {
		t.Fatal("appearance must still be saved and applied")
	}
	a.settingsOpen = false
	tt.Frame()
	if a.notice != "" || tt.HasText("外观设置已保存") {
		t.Fatal("appearance saves must not show a success notice")
	}
}

func TestOperationNoticesExpireInEveryView(t *testing.T) {
	for _, view := range []string{"workspace", "settings", "preview"} {
		t.Run(view, func(t *testing.T) {
			a, tt, _, _ := fixture(t, 1280, 900)
			switch view {
			case "settings":
				a.openSettings()
			case "preview":
				a.openArtwork(domain.ImageRecord{ID: "saved", Prompt: "山间小屋"})
				settle(a, tt)
			}
			const message = "图片已复制。"
			a.perform(func() (string, error) { return message, nil })
			settle(a, tt)
			if !tt.HasText(message) {
				t.Fatal("operation notice must be visible initially")
			}
			tt.Advance(3 * time.Second)
			if a.notice != "" || tt.HasText(message) {
				t.Fatal("operation notice must disappear automatically after three seconds")
			}
		})
	}
}

// A dialog with its own status area shows problems inside itself; the
// workspace banner stays hidden so the message is never duplicated or
// half-covered by the dialog.
func TestProblemShowsOnceWhileDialogIsOpen(t *testing.T) {
	for _, view := range []string{"workspace", "settings", "batch"} {
		t.Run(view, func(t *testing.T) {
			a, tt, _, _ := fixture(t, 1280, 900)
			switch view {
			case "settings":
				a.openSettings()
			case "batch":
				a.openBatch()
			}
			const message = "本机文件读取失败"
			a.problem = message
			tt.Frame()
			times := 0
			for _, text := range tt.Texts() {
				if strings.Contains(text, message) {
					times++
				}
			}
			if times != 1 {
				t.Fatalf("problem must render exactly once in %s, got %d", view, times)
			}
		})
	}
}

func TestRepeatedNoticeRestartsExpiryAndKeepsErrors(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	notify := func() {
		a.perform(func() (string, error) { return "图片已复制。", nil })
		settle(a, tt)
	}
	notify()
	tt.Advance(2 * time.Second)
	notify()
	tt.Advance(2 * time.Second)
	if !tt.HasText("图片已复制。") {
		t.Fatal("repeating an operation must restart the notice lifetime")
	}
	a.problem = "本机文件读取失败"
	tt.Frame()
	tt.Advance(time.Second)
	if a.notice != "" || !tt.HasText(a.problem) || a.problem == "" {
		t.Fatal("notice expiry must leave errors visible")
	}
}
