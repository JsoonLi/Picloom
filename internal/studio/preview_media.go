package studio

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

// The thumbnail session belongs to the dialog; full-image requests change on
// each selection. Switching images must not discard in-flight thumbnails.
type previewMedia struct {
	session    uint64
	selected   int // 0 is the result, 1..4 are saved references.
	result     *ui.Bitmap
	references []*ui.Bitmap
	problems   []string
	loading    bool
}

func (a *App) loadPreviewThumbnails(record domain.ImageRecord) {
	session := a.previewMedia.session
	count := len(a.previewMedia.references)
	needResult := a.previewMedia.result == nil
	a.previewMedia.loading = true
	a.run(func() func() {
		for selected := 0; selected <= count; selected++ {
			if selected == 0 && !needResult {
				continue
			}
			var data []byte
			var err error
			if selected == 0 {
				data, err = a.backend.ReadThumbnail(record.ID, referenceThumbSize, referenceThumbSize)
			} else {
				data, err = a.backend.ReadReference(record.ID, selected-1)
			}
			var bitmap *ui.Bitmap
			if err == nil {
				bitmap, err = thumbnail(data, referenceThumbSize, referenceThumbSize)
			}
			problem := ""
			if err != nil {
				problem = err.Error()
			}
			a.post(func() {
				if session != a.previewMedia.session || !a.previewOpen {
					return
				}
				a.previewMedia.problems[selected] = problem
				if selected == 0 {
					a.previewMedia.result = bitmap
				} else {
					a.previewMedia.references[selected-1] = bitmap
				}
			})
		}
		return func() {
			if session == a.previewMedia.session && a.previewOpen {
				a.previewMedia.loading = false
			}
		}
	})
}

func (a *App) selectPreviewImage(record domain.ImageRecord, selected int) {
	if !a.previewOpen || a.previewRecord == nil || a.previewRecord.ID != record.ID || selected < 0 || selected > len(a.previewMedia.references) {
		return
	}
	if a.previewMedia.selected == selected && (a.previewLoading || (a.previewBitmap != nil && a.problem == "")) {
		return
	}
	a.previewMedia.selected = selected
	a.previewRequest++
	request := a.previewRequest
	a.previewBitmap, a.previewData = nil, nil
	a.previewAnimation, a.markup = nil, nil
	a.previewEditable = false
	a.previewWidth, a.previewHeight = 0, 0
	a.previewLoading = true
	a.problem = ""
	a.viewer.reset()
	a.loadPreview(request, func() ([]byte, error) {
		if selected == 0 {
			return a.backend.ReadImage(record.ID)
		}
		return a.backend.ReadReference(record.ID, selected-1)
	}, nil)
}

func (a *App) previewImageStrip(c *ui.Context, record domain.ImageRecord) {
	ui.Text(c, "结果与参考图").FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	ui.Row(c.Key("preview-image-choices")).Label("预览图片选择").Role(ui.RoleRadioGroup).FillWidth().Wrap().Gap(8).Children(func() {
		for selected := 0; selected <= len(a.previewMedia.references); selected++ {
			label, key, bitmap := "结果图", "preview-result-thumbnail", a.previewMedia.result
			if selected > 0 {
				label, key = fmt.Sprintf("参考图 %d", selected), fmt.Sprintf("saved-reference-%d", selected-1)
				bitmap = a.previewMedia.references[selected-1]
			}
			current := selected == a.previewMedia.selected
			border, background := ui.Transparent, ui.Transparent
			if current {
				border, background = c.Theme().Focus, c.Theme().SurfaceHover
			}
			button := ui.Button(c.Key(key), "").Label(label).Tooltip("放大查看"+label).Role(ui.RoleRadio).Checked(current).Size(76, 92).Padding(4).Border(2, border).Background(background).Radius(10)
			button.Children(func() {
				ui.Column(c).Center().Gap(4).Children(func() {
					ui.Box(c).Size(60, 60).Radius(6).Clip().Background(c.Theme().Surface).Children(func() {
						if bitmap != nil {
							ui.Image(c, bitmap).Fill().Fit(ui.Contain)
						} else {
							message := "加载中"
							if selected < len(a.previewMedia.problems) && a.previewMedia.problems[selected] != "" {
								message = "不可用"
							}
							ui.Text(c, message).Fill().TextAlign(ui.Center).FontSize(textSize(c, 11)).TextColor(c.Theme().TextMuted)
						}
						if current {
							ui.Text(c, "✓").Absolute().Top(2).Right(2).Size(16, 16).TextAlign(ui.Center).FontSize(11).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).Radius(8)
						}
					})
					ui.Text(c, label).FontSize(textSize(c, 11)).SingleLine()
				})
			})
			button.OnClick(func() { a.selectPreviewImage(record, selected) })
		}
	})
}
