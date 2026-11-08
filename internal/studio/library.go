package studio

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

const (
	artworkMaxCardWidth   = 180
	artworkCellHeight     = 252
	artworkMaxImageHeight = 160
	artworkGridGap        = 12
	artworkCardHeight     = 240
	artworkCardPadding    = 10
	// Padding, border, two gaps, up to two caption lines and the action row.
	artworkCardDetailsHeight = 2*artworkCardPadding + 2 + 12 + 36 + 24
)

func (a *App) library(c *ui.Context, narrow bool) {
	width, _ := c.Size()
	gutter := float32(36)
	if narrow {
		gutter = 20
	}
	available := min(float32(1440), max(float32(200), width-gutter*2))
	favoriteFilter := func() {
		favoriteText := "收藏"
		if a.history.FavoriteCount > 0 {
			favoriteText += fmt.Sprintf(" %d", a.history.FavoriteCount)
		}
		favorite := quietButton(c, "favorites-only", "").Label("只看收藏").Height(32).Padding(4, 12)
		favorite.Children(func() {
			ui.Row(c).Center().Gap(8).Children(func() { ui.Icon(c, icons["star"]).Size(16, 16); ui.Text(c, favoriteText) })
		})
		if a.favoritesOnly {
			favorite.Background(c.Theme().SurfacePressed)
		}
		if favorite.Clicked() {
			a.favoritesOnly = !a.favoritesOnly
			a.refreshHistory()
		}
	}
	ui.Column(c.Key("library")).FillWidth().Grow(1).MinHeight(0).Gap(16).Children(func() {
		ui.Row(c.Key("library-title")).FillWidth().Wrap().Gap(12).Children(func() {
			title := ui.Row(c.Key("library-heading")).Gap(12).Height(32).AlignItems(ui.Center)
			if narrow {
				title.FillWidth()
			}
			title.Children(func() {
				ui.Text(c, "作品库").FontSize(textSize(c, 18)).FontWeight(600)
				ui.Text(c.Key("history-count"), fmt.Sprintf("%d 张", a.history.Total)).Label(fmt.Sprintf("%d 张作品 · %d 张收藏", a.history.Total, a.history.FavoriteCount)).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
				if narrow {
					ui.Spacer(c)
					favoriteFilter()
				}
			})
			if !narrow {
				ui.Spacer(c)
			}
			tools := ui.Row(c.Key("library-tools")).Gap(8)
			if narrow {
				tools.FillWidth()
			}
			tools.Children(func() {
				search := ui.Box(c).Width(224).MinWidth(0).Height(32)
				if narrow {
					search.Grow(1)
				}
				search.Children(func() {
					if ui.TextInput(c.Key("history-search"), &a.search).Label("搜索作品").Placeholder("搜索提示词或模型").FillWidth().Height(32).Padding(4, 12, 4, 36).Changed() {
						a.refreshHistory()
					}
					ui.Icon(c, icons["search"]).Size(16, 16).Absolute().Left(12).Top(8).TextColor(c.Theme().TextMuted)
				})
				if !narrow {
					favoriteFilter()
				}
				settings := quietButton(c, "open-settings", "").Label("打开设置").Tooltip("设置").Height(32).Padding(4, 8)
				settings.Children(func() {
					ui.Row(c).Center().Gap(8).Children(func() {
						ui.Icon(c, icons["settings"]).Size(16, 16)
						ui.Text(c, "设置")
					})
				})
				if settings.Clicked() {
					a.openSettings()
				}
			})
		})
		n := len(a.jobs) + len(a.history.Items)
		if n == 0 {
			message := "写下提示词，开始生成。作品与参考图会自动保存在本机。"
			title := "还没有作品"
			if a.historyLoading {
				title = "正在读取作品库"
				message = "请稍候…"
			} else if a.search != "" || a.favoritesOnly {
				title = "没有匹配的作品"
				message = "试试其他搜索词，或取消收藏筛选"
			}
			ui.Scroll(c.Key("library-empty")).Label("作品库空状态").FillWidth().Height(224).Shrink(1).MinHeight(0).Center().Gap(16).Border(1, c.Theme().Border).Radius(20).Children(func() {
				ui.Text(c, title).FontSize(textSize(c, 16)).FontWeight(500)
				ui.Text(c.Key("history-empty"), message).MaxWidth(min(520, available-32)).TextAlign(ui.Center).TextColor(c.Theme().TextMuted)
				if !a.historyLoading && a.search == "" && !a.favoritesOnly {
					if ui.Button(c.Key("empty-settings"), "配置模型服务").Height(40).Clicked() {
						a.openSettings()
					}
				}
			})
		} else {
			columns := max(1, int((available-8+artworkGridGap)/(artworkMaxCardWidth+artworkGridGap)))
			a.grid.Selected = nil
			a.grid.Key = func(i int) any {
				i *= columns
				if i < len(a.jobs) {
					return "job-" + a.jobs[i].Args.JobID
				}
				index := i - len(a.jobs)
				if index >= 0 && index < len(a.history.Items) {
					return a.history.Items[index].ID
				}
				return i
			}
			a.grid.Label = func(i int) string {
				i *= columns
				if i < len(a.jobs) {
					return a.jobs[i].Args.Prompt
				}
				index := i - len(a.jobs)
				if index >= 0 && index < len(a.history.Items) {
					return a.history.Items[index].Prompt
				}
				return ""
			}
			cellHeight := float32(artworkCellHeight)
			for _, record := range a.history.Items {
				_, imageHeight := a.artworkImageSize(record)
				cellHeight = max(cellHeight, imageHeight+artworkCardDetailsHeight+12)
			}
			// The grid scrolls itself so only the rows on screen, and a few
			// around them, are built: hundreds of artworks cost what a
			// screenful does.
			a.beginThumbnailFrame()
			// Virtualize whole rows so cards keep the same gap even in an
			// incomplete row; spare width stays at the right.
			rows := (n + columns - 1) / columns
			ui.List(c.Key("artwork-grid"), &a.grid, rows, func(row int) {
				ui.Row(c).FillWidth().Height(cellHeight).Gap(artworkGridGap).Padding(4).Children(func() {
					for i := row * columns; i < min(n, (row+1)*columns); i++ {
						ui.Box(c.Key(a.gridItemKey(i))).Width(artworkMaxCardWidth).Shrink(0).Children(func() {
							if i < len(a.jobs) {
								a.jobCard(c, a.jobs[i])
							} else {
								a.artworkCard(c, a.history.Items[i-len(a.jobs)])
							}
						})
					}
				})
			}).FillWidth().Grow(1).MinHeight(min(float32(240), cellHeight)).Label("作品和生成任务")
			a.pumpThumbnails()
		}
	})
}

func (a *App) gridItemKey(i int) string {
	if i < len(a.jobs) {
		return "job-" + a.jobs[i].Args.JobID
	}
	return a.history.Items[i-len(a.jobs)].ID
}

func (a *App) jobCard(c *ui.Context, task *job) {
	glassPane(c, "job-"+task.Args.JobID).Padding(artworkCardPadding).Gap(6).Width(artworkMaxCardWidth).MaxWidthPercent(100).Height(artworkCardHeight).Radius(12).Children(func() {
		ui.Text(c, task.Args.Prompt).MaxLines(2)
		ui.Text(c, task.Args.Model).FontSize(textSize(c, 12)).SingleLine().TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		ui.Text(c.Key("job-stage-"+task.Args.JobID), task.Progress.Stage).FontWeight(500)
		if task.Progress.Status == "error" {
			ui.TextArea(c.Key("job-error-"+task.Args.JobID), &task.Progress.Error).ReadOnly(true).Label("生成失败原因").Tooltip(task.Progress.Error).FillWidth().Height(64).Padding(0).Border(0, ui.Transparent).Background(ui.Transparent).TextColor(c.Theme().Danger)
			ui.Row(c).Wrap().Gap(6).Children(func() {
				ui.Button(c.Key("retry-"+task.Args.JobID), "重试").Height(32).Padding(4, 10).OnClick(func() {
					args := task.Args
					args.JobID = newID()
					a.removeJob(task)
					a.startJob(args, task.ProviderName)
				})
				ui.Button(c.Key("dismiss-job-"+task.Args.JobID), "移除任务").Height(32).Padding(4, 10).OnClick(func() { a.removeJob(task) })
			})
		} else {
			ui.Progress(c.Key("job-progress-"+task.Args.JobID), float64(task.Progress.Progress)/100).Label("生成进度")
			ui.Text(c, "进度表示请求阶段\n完成后自动保存").FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		}
	})
}

func (a *App) removeJob(task *job) {
	for i, j := range a.jobs {
		if j == task {
			a.jobs = append(a.jobs[:i], a.jobs[i+1:]...)
			return
		}
	}
}

func (a *App) artworkImageSize(record domain.ImageRecord) (float32, float32) {
	// The record's size keeps cells steady while thumbnails arrive; the
	// thumbnail only stands in for legacy records without one.
	w, h := record.Width, record.Height
	if thumb := a.thumbs[record.ID]; thumb != nil && (w <= 0 || h <= 0) {
		w, h = thumb.Size()
	}
	if w <= 0 || h <= 0 {
		w, h = 1, 1
	}
	width := float32(artworkMaxCardWidth - 2*artworkCardPadding - 2)
	return width, min(float32(artworkMaxImageHeight), width*float32(h)/float32(w))
}

func (a *App) artworkCard(c *ui.Context, record domain.ImageRecord) {
	imageWidth, imageHeight := a.artworkImageSize(record)
	glassPane(c, "artwork-"+record.ID).Radius(12).Padding(artworkCardPadding).Gap(6).Width(artworkMaxCardWidth).MaxWidthPercent(100).Children(func() {
		a.wantThumbnail(record.ID)
		thumb := a.thumbs[record.ID]
		ui.Box(c.Key("artwork-well-" + record.ID)).FillWidth().Height(imageHeight).Center().Children(func() {
			ui.Box(c.Key("artwork-image-frame-"+record.ID)).Size(imageWidth, imageHeight).Radius(6).Clip().Children(func() {
				if thumb != nil {
					if ui.Image(c.Key("artwork-image-"+record.ID), thumb).Fill().Fit(ui.Cover).Label("预览作品 " + record.Prompt).Focusable().Clicked() {
						a.openArtwork(record)
					}
				} else {
					text := "正在读取图片…"
					if a.thumbErrors[record.ID] != "" {
						text = "图片无法读取"
					}
					ui.Box(c).Fill().Center().Children(func() { ui.Text(c, text).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted) })
				}
				favorite, star := "收藏", "star"
				if record.Favorite {
					favorite, star = "取消收藏", "star-filled"
				}
				iconButton(c, "favorite-"+record.ID, favorite+" "+record.ID, star).Absolute().Top(4).Right(4).Size(24, 24).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(6).OnClick(func() { a.toggleFavorite(record) })
				iconButton(c, "preview-"+record.ID, "查看作品 "+record.ID, "expand").Absolute().Bottom(4).Right(4).Size(24, 24).Background(ui.RGBA(20, 20, 20, 0.6)).TextColor(ui.Hex("#ffffff")).Radius(6).OnClick(func() { a.openArtwork(record) })
			})
		})
		ui.Text(c, record.Prompt).MaxLines(2).FontSize(textSize(c, 13))
		ui.Row(c).FillWidth().Gap(2).Children(func() {
			ui.Text(c, record.Model).SingleLine().Grow(1).FontSize(textSize(c, 11)).TextColor(c.Theme().TextMuted)
			iconButton(c, "reuse-"+record.ID, "复用参数 "+record.ID, "images").Size(24, 24).TextColor(c.Theme().TextMuted).OnClick(func() { a.reuse(record) })
			iconButton(c, "export-"+record.ID, "导出原图 "+record.ID, "download").Size(24, 24).TextColor(c.Theme().TextMuted).OnClick(func() {
				a.perform(func() (string, error) {
					saved, err := a.desktop.ExportImage(record.ID)
					if saved && err == nil {
						return "图片已导出。", nil
					}
					return "", err
				})
			})
			iconButton(c, "delete-"+record.ID, "删除作品 "+record.ID, "trash").Size(24, 24).TextColor(c.Theme().TextMuted).OnClick(func() { a.deleteID = record.ID; a.deleteOpen = true })
		})
	})
}

func (a *App) refreshHistory() {
	a.historyRequest++
	request := a.historyRequest
	query := domain.HistoryQuery{All: true, Search: a.search, FavoritesOnly: a.favoritesOnly}
	a.historyLoading = true
	a.run(func() func() {
		page, err := a.backend.ListHistory(query)
		return func() {
			if request != a.historyRequest {
				return
			}
			a.historyLoading = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.setHistory(page)
		}
	})
}

// setHistory shows the matching records; thumbnails stay cached so search
// and favourite changes show known artworks at once.
func (a *App) setHistory(page domain.HistoryPage) {
	a.history = page
	a.thumbErrors = make(map[string]string)
	a.trimThumbs()
}

func (a *App) toggleFavorite(record domain.ImageRecord) {
	a.run(func() func() {
		_, err := a.backend.Favorite(record.ID, !record.Favorite)
		return func() {
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.refreshHistory()
		}
	})
}

func (a *App) deleteArtwork(id string) {
	a.run(func() func() {
		err := a.backend.Delete(id)
		return func() {
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.forgetThumb(id)
			if a.previewRecord != nil && a.previewRecord.ID == id {
				a.previewOpen = false
			}
			a.showNotice("作品已删除。")
			a.refreshHistory()
		}
	})
}

// beginPreview opens the viewer at once with placeholder, a thumbnail
// already in memory, drawn at the original's size w×h until the full image
// decodes. Without a known size there is no placeholder: a small bitmap
// would show at its own size and then jump.
func (a *App) beginPreview(title string, record *domain.ImageRecord, placeholder *ui.Bitmap, w, h int) uint64 {
	a.notice, a.problem = "", ""
	a.viewer.reset()
	a.previewRequest++
	a.previewOpen = true
	a.previewRecord = record
	a.previewTitle = title
	a.previewBitmap, a.previewWidth, a.previewHeight = nil, 0, 0
	a.previewData, a.markup = nil, nil
	a.previewEditable = false
	a.previewAnimation = nil
	if placeholder != nil && w > 0 && h > 0 {
		a.previewBitmap, a.previewWidth, a.previewHeight = placeholder, float32(w), float32(h)
	}
	a.previewReferences = nil
	a.previewLoading = true
	return a.previewRequest
}

// loadPreview decodes the full image off the UI thread and shows it as
// soon as it is ready, before any slower follow-up work.
func (a *App) loadPreview(request uint64, read func() ([]byte, error), then func()) {
	a.run(func() func() {
		data, err := read()
		var image *ui.Bitmap
		var animation *previewAnimation
		var originalWidth, originalHeight float32
		var editable bool
		if err == nil {
			format, formatErr := generation.ImageFormat(data)
			editable = format == "png" || format == "jpg" || format == "webp"
			if formatErr != nil {
				err = formatErr
			} else if format == "gif" {
				decoded, e := generation.DecodeAnimation(data)
				err = e
				if e == nil {
					animation = newPreviewAnimation(decoded)
					image = animation.frames[0]
					if len(animation.frames) < 2 {
						animation = nil
					}
				}
			} else if format == "svg" {
				decoded, e := generation.Thumbnail(data, generation.MaxThumbnailSide, generation.MaxThumbnailSide)
				err = e
				if e == nil {
					image = ui.NewBitmap(decoded)
					size, _ := generation.ImageSize(data)
					originalWidth, originalHeight = float32(size.X), float32(size.Y)
				}
			} else {
				image, err = ui.DecodeBitmap(data)
			}
		}
		a.post(func() {
			if request != a.previewRequest {
				return
			}
			a.previewLoading = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.previewBitmap = image
			a.previewData = data
			a.previewEditable = editable
			a.previewAnimation = animation
			a.previewWidth, a.previewHeight = originalWidth, originalHeight
			if then != nil {
				then()
			}
		})
		return nil
	})
}

func (a *App) openArtwork(record domain.ImageRecord) {
	request := a.beginPreview("作品详情", &record, a.thumbs[record.ID], record.Width, record.Height)
	a.loadPreview(request, func() ([]byte, error) { return a.backend.ReadImage(record.ID) }, func() {
		if record.ReferenceCount == 0 {
			return
		}
		a.run(func() func() {
			var refs []*ui.Bitmap
			var err error
			for i := 0; i < record.ReferenceCount; i++ {
				var data []byte
				var thumb *ui.Bitmap
				if data, err = a.backend.ReadReference(record.ID, i); err == nil {
					thumb, err = thumbnail(data, referenceThumbSize, referenceThumbSize)
				}
				if err != nil {
					break
				}
				refs = append(refs, thumb)
			}
			return func() {
				if request != a.previewRequest {
					return
				}
				if err != nil {
					a.problem = err.Error()
					return
				}
				a.previewReferences = refs
			}
		})
	})
}

func (a *App) openReference(reference domain.Reference) {
	a.beginPreview(reference.Name, nil, nil, 0, 0)
	a.loadMarkup(func() ([]byte, error) { return referenceBytes(reference) }, true)
}

// releasePreview drops the full-resolution preview once the viewer closes
// and abandons decodes still running for it.
func (a *App) releasePreview() {
	if a.previewOpen || (a.previewBitmap == nil && a.previewReferences == nil && !a.previewLoading && a.markup == nil) {
		return
	}
	a.previewRequest++
	a.previewBitmap, a.previewReferences, a.previewLoading = nil, nil, false
	a.previewAnimation = nil
	a.previewWidth, a.previewHeight = 0, 0
	a.previewData, a.markup = nil, nil
	a.previewEditable = false
}

const previewMaxWidth = 1280

func (a *App) preview(c *ui.Context, narrow bool) {
	restore := popoverTheme(c)
	defer restore()
	width, height := c.Size()
	contentWidth := min(float32(previewMaxWidth-40), max(float32(240), width-80))
	contentHeight := max(float32(240), height-80)
	wide := width >= 900 && (a.previewRecord != nil || a.markup != nil)
	defer a.releasePreview()
	ui.Modal(c.Key("preview-dialog"), &a.previewOpen, func() {
		ui.Column(c.Key("preview-panel")).Width(contentWidth).Height(contentHeight).MaxWidthPercent(100).Gap(8).Children(func() {
			ui.Row(c).FillWidth().Shrink(0).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Grow(1).MinWidth(0).Wrap().Gap(12).AlignItems(ui.Center).Children(func() {
					title := a.previewTitle
					if a.markup != nil {
						title = "标注编辑"
					}
					ui.Text(c, title).FontSize(textSize(c, 16)).FontWeight(600)
					if r := a.previewRecord; r != nil {
						ui.Text(c, r.ProviderName+" / "+r.Model).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
					}
				})
				if iconButton(c, "close-preview", "关闭", "close").Size(28, 28).Shrink(0).Clicked() {
					a.previewOpen = false
				}
			})
			ui.Divider(c).Shrink(0)
			if a.markup != nil {
				a.markupContent(c, wide, contentHeight)
				return
			}
			renderImage := func() {
				if a.previewRecord == nil && a.problem != "" {
					ui.Text(c.Key("reference-preview-error"), a.problem).TextColor(c.Theme().Danger)
				}
				if a.previewLoading {
					ui.Text(c, "正在读取原图…").TextColor(c.Theme().TextMuted)
				}
				if a.previewBitmap != nil {
					a.imageViewer(c)
				}
			}
			if wide {
				ui.Row(c.Key("preview-content")).Grow(1).MinHeight(0).FillWidth().AlignItems(ui.Stretch).Children(func() {
					ui.Column(c.Key("preview-artwork-panel")).Grow(1).MinWidth(0).Gap(8).Padding(8).Radius(12).Background(toolbarColor(c)).Children(renderImage)
					ui.Column(c.Key("preview-metadata-panel")).Width(310).Shrink(0).Padding(0, 0, 0, 20).Gap(16).Children(func() {
						ui.Scroll(c.Key("preview-metadata-scroll")).Grow(1).MinHeight(0).FillWidth().Gap(14).Children(func() { a.previewDetails(c, *a.previewRecord) })
						a.previewActions(c, *a.previewRecord)
					})
				})
			} else {
				ui.Scroll(c.Key("preview-content")).Grow(1).MinHeight(0).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(16).Children(func() {
						ui.Column(c.Key("preview-artwork-panel")).FillWidth().Height(min(float32(600), max(float32(240), contentHeight*0.65))).Shrink(0).Gap(8).Padding(8).Radius(12).Background(toolbarColor(c)).Children(renderImage)
						if record := a.previewRecord; record != nil {
							a.previewDetails(c, *record)
							a.previewActions(c, *record)
						}
					})
				})
			}
		})
	})
}
func (a *App) previewDetails(c *ui.Context, record domain.ImageRecord) {
	if a.problem != "" {
		ui.Text(c.Key("preview-error"), a.problem).TextColor(c.Theme().Danger)
	}
	if a.notice != "" {
		ui.Text(c.Key("preview-notice"), a.notice).TextColor(c.Theme().TextMuted)
	}
	if a.previewTitle != "作品详情" {
		quietButton(c, "preview-original", "查看生成原图").Height(40).OnClick(func() { a.openArtwork(record) })
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, "提示词").FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		ui.Spacer(c)
		quietButton(c, "copy-prompt", "复制提示词").Height(32).Padding(4, 8).OnClick(func() {
			a.perform(func() (string, error) { return "提示词已复制。", a.desktop.CopyText(record.Prompt) })
		})
	})
	prompt := record.Prompt
	ui.TextArea(c.Key("preview-prompt"), &prompt).ReadOnly(true).Label("作品提示词").Height(110).FillWidth().Padding(0).Border(0, ui.Transparent).Background(ui.Transparent)
	ui.Row(c).Wrap().Gap(8).Children(func() {
		ui.Text(c, artworkSizeLabel(record)).FontSize(textSize(c, 11)).Padding(2, 8).Background(c.Theme().SurfacePressed).Radius(8)
		ui.Text(c, strings.ToUpper(strings.TrimPrefix(record.MIMEType, "image/"))).FontSize(textSize(c, 11)).Padding(2, 8).Background(c.Theme().SurfacePressed).Radius(8)
	})
	a.gifActualTiming(c, record)
	ui.Text(c, time.UnixMilli(record.CreatedAt).Format("2006/1/2 15:04:05")).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
	if len(a.previewReferences) > 0 {
		ui.Text(c, "生成时使用的参考图").FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
		ui.Row(c).Wrap().Gap(8).Children(func() {
			for i, thumb := range a.previewReferences {
				index := i
				ui.Image(c.Key(fmt.Sprintf("saved-reference-%d", i)), thumb).Size(60, 60).Fit(ui.Contain).Radius(8).Border(1, c.Theme().Border).Label(fmt.Sprintf("参考图 %d", i+1)).Focusable().OnClick(func() { a.openSavedReference(record, index) })
			}
		})
	}
}

func (a *App) previewActions(c *ui.Context, record domain.ImageRecord) {
	buttonContent := func(icon, label string) {
		ui.Row(c).Center().Gap(8).Children(func() {
			ui.Icon(c, icons[icon]).Size(16, 16)
			ui.Text(c, label).FontSize(textSize(c, 14)).SingleLine()
		})
	}
	ui.Column(c.Key("preview-actions")).Shrink(0).FillWidth().Gap(12).Children(func() {
		ui.Divider(c)
		ui.Grid(c).Columns(2).Gap(8).FillWidth().Children(func() {
			copyButton := ui.Button(c.Key("copy-image"), "").Label("复制图片").Tooltip("复制图片").Height(40).FillWidth().Padding(0)
			copyButton.Children(func() { buttonContent("copy", "复制图片") })
			copyButton.OnClick(func() {
				a.perform(func() (string, error) { return "图片已复制。", a.desktop.CopyImage(record.ID) })
			})
			exportButton := ui.Button(c.Key("export-image"), "").Label("导出原图").Tooltip("导出原图").Height(40).FillWidth().Padding(0)
			exportButton.Children(func() { buttonContent("download", "导出原图") })
			exportButton.OnClick(func() {
				a.perform(func() (string, error) {
					saved, err := a.desktop.ExportImage(record.ID)
					if saved && err == nil {
						return "图片已导出。", nil
					}
					return "", err
				})
			})
			favorite, star := "收藏", "star"
			if record.Favorite {
				favorite, star = "取消收藏", "star-filled"
			}
			favoriteButton := ui.Button(c.Key("preview-favorite"), "").Label(favorite).Tooltip(favorite).Height(40).FillWidth().Padding(0)
			favoriteButton.Children(func() { buttonContent(star, favorite) })
			favoriteButton.OnClick(func() { a.toggleFavorite(record) })
			reuseButton := ui.PrimaryButton(c.Key("reuse-preview"), "").Label("复用参数").Tooltip("做同款").Height(40).FillWidth().Padding(0)
			reuseButton.Children(func() { buttonContent("images", "做同款") })
			reuseButton.OnClick(func() { a.reuse(record); a.previewOpen = false })
		})
	})
}

func (a *App) openSavedReference(record domain.ImageRecord, index int) {
	refs := a.previewReferences
	request := a.beginPreview(fmt.Sprintf("参考图 %d", index+1), &record, nil, 0, 0)
	// Keep the reference strip so switching between references stays instant.
	a.previewReferences = refs
	a.loadPreview(request, func() ([]byte, error) { return a.backend.ReadReference(record.ID, index) }, nil)
}

func (a *App) perform(operation func() (string, error)) {
	a.run(func() func() {
		message, err := operation()
		return func() {
			if err != nil {
				a.problem = err.Error()
			} else if message != "" {
				a.showNotice(message)
			}
		}
	})
}

func (a *App) reuse(record domain.ImageRecord) {
	if a.importing {
		return
	}
	a.referenceEpoch++
	epoch := a.referenceEpoch
	a.importing = true
	a.run(func() func() {
		refs := []domain.Reference{}
		thumbs := make(map[string]*ui.Bitmap)
		var err error
		for i := 0; i < record.ReferenceCount; i++ {
			var data []byte
			data, err = a.backend.ReadReference(record.ID, i)
			if err != nil {
				break
			}
			ref := domain.Reference{ID: newID(), Name: fmt.Sprintf("参考图 %d", i+1), DataURL: "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)}
			thumbs[ref.ID], err = thumbnail(data, referenceThumbSize, referenceThumbSize)
			if err != nil {
				break
			}
			refs = append(refs, ref)
		}
		return func() {
			if epoch != a.referenceEpoch {
				return
			}
			a.importing = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.prompt, _ = compactPrompt(record.Prompt, [2]int{})
			a.providerID = record.ProviderID
			a.model = record.Model
			a.outputFormat = ""
			if record.MIMEType == "image/gif" {
				a.outputFormat = "gif"
				a.reuseGIFSettings(record)
			}
			if record.MIMEType == "image/svg+xml" {
				a.outputFormat = "svg"
			}
			a.references = refs
			a.referenceThumbs = thumbs
			a.ratio, a.resolution = generation.FindSizeCombo(record.Size)
			a.reconcileSelection()
			a.savePreferences()
			a.loadPricing()
			a.showNotice("已复用提示词、参考图和生成参数。")
		}
	})
}
