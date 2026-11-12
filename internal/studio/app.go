// Package studio contains the native, image-first Image2AI workspace.
package studio

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"image2ai/internal/locale"
	"math"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/webp"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

var ratios = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}

type job struct {
	Args         domain.GenerateArgs
	Progress     domain.Progress
	ProviderName string
	running      bool
}

// App owns draft and running-job state independently of transient dialogs.
// All model fields are changed on the UI thread; workers post completed values.
type App struct {
	backend                                      domain.Backend
	desktop                                      domain.Desktop
	updates                                      *updatePanel
	updateRestoreFailed                          bool
	ctx                                          context.Context
	cancel                                       context.CancelFunc
	windowMu                                     sync.RWMutex
	window                                       *mygo.Window
	pending                                      chan func()
	workers                                      sync.WaitGroup
	settingsWrites                               sync.WaitGroup
	started, loaded, importing                   bool
	config                                       domain.Config
	prompt, providerID, model, ratio, resolution string
	outputFormat                                 string
	draftSize, draftMarkup                       string
	gifSize                                      string
	gifFrameCount, gifFrameDurationMS            int
	preferencesSaving                            bool
	pendingPreferences                           *domain.GenerationPreferences
	references                                   []domain.Reference
	referenceThumbs                              map[string]*ui.Bitmap
	jobs                                         []*job
	pricing                                      map[string][]domain.ModelPricing
	priceLoading                                 map[string]bool
	pricingEpoch                                 uint64
	notice, problem                              string
	noticeExpiresAt                              time.Time
	search                                       string
	favoritesOnly                                bool
	history                                      domain.HistoryPage
	historyLoading                               bool
	historyRequest                               uint64
	referenceEpoch                               uint64
	grid                                         ui.ListState
	thumbs                                       map[string]*ui.Bitmap
	thumbLoading                                 map[string]bool
	thumbErrors                                  map[string]string
	thumbUsed                                    map[string]uint64
	thumbBytes                                   int
	thumbFrame                                   uint64
	thumbWanted                                  []string
	thumbInFlight                                int
	previewOpen                                  bool
	previewRecord                                *domain.ImageRecord
	previewBitmap                                *ui.Bitmap
	previewData                                  []byte
	previewEditable                              bool
	markup                                       *markupEditor
	previewAnimation                             *previewAnimation
	previewWidth, previewHeight                  float32
	previewMedia                                 previewMedia
	previewLoading                               bool
	previewTitle                                 string
	previewRequest                               uint64
	viewer                                       imageView
	deleteOpen                                   bool
	deleteID                                     string
	settingsOpen                                 bool
	settingsTab                                  string
	settingsAdvanced                             bool
	draftProviders                               []domain.Provider
	draftProviderID                              string
	draftModels                                  map[string]string
	draftModelOptions                            map[string][]string
	draftProxy                                   domain.ProxyConfig
	draftTheme                                   string
	draftLanguage                                string
	saving                                       bool
	discoverBusy                                 bool
	modelRequest                                 uint64
	providerDeleteOpen                           bool
	autosave                                     settingsAutosave
	showAPIKey                                   bool
	batch                                        *batchState
	batchRuns                                    []*batchRun
	batchSaves                                   map[string]map[string]domain.BatchItem
	batchInFlight                                map[string]map[string]domain.BatchItem
	batchWrites                                  sync.WaitGroup
	batchSaving                                  bool
	libraryCache                                 []libraryEntry
	libraryCacheKey                              libraryKey
}

func New(backend domain.Backend, desktop domain.Desktop) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{backend: backend, desktop: desktop, ctx: ctx, cancel: cancel, pending: make(chan func(), 256),
		gifSize: generation.DefaultGIFSize, gifFrameCount: generation.DefaultGIFFrameCount, gifFrameDurationMS: generation.DefaultGIFFrameDurationMS,
		ratio: "3:4", resolution: "2k", pricing: make(map[string][]domain.ModelPricing), priceLoading: make(map[string]bool),
		history: domain.HistoryPage{Page: 1, PageSize: 12, TotalPages: 1},
		thumbs:  make(map[string]*ui.Bitmap), thumbLoading: make(map[string]bool), thumbErrors: make(map[string]string),
		thumbUsed:       make(map[string]uint64),
		referenceThumbs: make(map[string]*ui.Bitmap), settingsTab: "提供商"}
}

func (a *App) SetWindow(w *mygo.Window) { a.windowMu.Lock(); a.window = w; a.windowMu.Unlock() }

// Close cancels remote requests and finishes local settings and batch writes
// before the owner closes storage. UI callbacks need not run during shutdown.
func (a *App) Close() {
	a.cancel()
	a.settingsWrites.Wait()
	a.batchWrites.Wait()
	a.flushBatchSavesOnClose()
	a.flushSettingsOnClose()
}

func (a *App) post(fn func()) {
	select {
	case a.pending <- fn:
	case <-a.ctx.Done():
		return
	}
	a.windowMu.RLock()
	w := a.window
	a.windowMu.RUnlock()
	if w != nil {
		w.Update(a.drain)
	}
}

func (a *App) drain() {
	for {
		select {
		case fn := <-a.pending:
			fn()
		default:
			return
		}
	}
}

func (a *App) run(fn func() func()) {
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		done := fn()
		if done != nil {
			a.post(done)
		}
	}()
}

func (a *App) start() {
	a.started = true
	a.historyLoading = true
	a.run(func() func() {
		config, err := a.backend.LoadConfig()
		var page domain.HistoryPage
		if err == nil {
			page, err = a.backend.ListHistory(domain.HistoryQuery{All: true})
		}
		draft, draftErr := a.desktop.LoadUpdateDraft()
		var draftThumbs map[string]*ui.Bitmap
		if draft != nil && draftErr == nil {
			draftThumbs, draftErr = updateDraftThumbnails(draft)
		}
		return func() {
			a.historyLoading = false
			if err != nil {
				a.problem = err.Error()
				return
			}
			a.loaded = true
			a.config = config
			a.setHistory(page)
			a.loadBatchRuns()
			p := config.GenerationPreferences
			a.outputFormat = p.OutputFormat
			a.gifSize = p.GIFSize
			if a.gifSize == "" {
				a.gifSize = generation.DefaultGIFSize
			}
			a.gifFrameCount = p.GIFFrameCount
			if a.gifFrameCount == 0 {
				a.gifFrameCount = generation.DefaultGIFFrameCount
			}
			a.gifFrameDurationMS = p.GIFFrameDurationMS
			if a.gifFrameDurationMS == 0 {
				a.gifFrameDurationMS = generation.DefaultGIFFrameDurationMS
			}
			a.providerID, a.model, a.ratio, a.resolution = p.ProviderID, p.Model, p.Ratio, p.Resolution
			if draftErr != nil {
				a.updateRestoreFailed = true
				a.problem = draftErr.Error()
			} else if draft != nil {
				a.restoreUpdateDraft(draft, draftThumbs)
				a.run(func() func() {
					err := a.desktop.ClearUpdateDraft()
					return func() {
						if err != nil {
							a.problem = err.Error()
						}
					}
				})
			}
			a.reconcileSelection()
			a.loadPricing()
		}
	})
}

func (a *App) View(c *ui.Context) {
	studioTheme(c)
	a.drain()
	a.applyPendingUpdate()
	a.updateNotice(c)
	if !a.started {
		a.start()
	}
	width, _ := c.Size()
	narrow := width < 600
	gutter := float32(36)
	top := float32(8)
	if narrow {
		gutter = 20
	}
	ui.Column(c.Key("workspace")).Fill().Disabled(a.updateRestarting()).Background(c.Theme().Background).Children(func() {
		a.titlebar(c)
		// The gallery grid scrolls itself; the header above it stays put.
		ui.Column(c.Key("workspace-body")).Grow(1).MinHeight(0).FillWidth().Padding(top, gutter, 16, gutter).AlignItems(ui.Center).Children(func() {
			ui.Column(c.Key("studio-content")).FillWidth().MaxWidth(1440).Grow(1).MinHeight(0).Children(func() {
				if !a.loaded {
					ui.Column(c).Padding(40, 0).Gap(16).Children(func() {
						if a.historyLoading {
							ui.Text(c, locale.T("正在打开工作台…")).TextColor(c.Theme().TextMuted)
						} else {
							a.messages(c)
							if ui.Button(c.Key("reload-startup"), locale.T("重新读取")).Clicked() {
								a.start()
							}
						}
					})
					return
				}
				a.messages(c)
				a.library(c, narrow)
			})
		})
		if a.loaded {
			composerWidth := float32(960)
			if a.outputFormat == "gif" {
				composerWidth = 1200
			}
			ui.Row(c.Key("composer-dock")).FillWidth().Justify(ui.Center).Padding(12, gutter, 20, gutter).Children(func() {
				ui.Column(c.Key("composer-section")).FillWidth().MaxWidth(composerWidth).Children(func() {
					a.composer(c, narrow)
				})
			})
		}
	})
	if a.notice != "" && !a.settingsOpen && !a.previewOpen {
		ui.Row(c).Absolute().Top(52).Left(0).Right(0).Center().Children(func() {
			glassPane(c, "notice-pane").MaxWidth(max(200, width-40)).Padding(10, 14).Radius(14).Children(func() {
				ui.Row(c).Gap(10).Children(func() {
					ui.Text(c.Key("notice"), a.notice).FontSize(textSize(c, 13))
					if iconButton(c, "dismiss-notice", locale.T("关闭通知"), "close").Size(26, 26).Clicked() {
						a.notice = ""
					}
				})
			})
		})
	}
	a.batchPanel(c, narrow)
	a.settings(c, narrow)
	a.runSettingsSaves(c)
	a.preview(c, narrow)
	if ui.AlertDialog(c.Key("delete-artwork-dialog"), &a.deleteOpen, locale.T("删除作品？"), locale.T("作品和关联参考图将从本机删除，此操作无法撤销。"), locale.T("取消"), locale.T("删除作品")) == 1 {
		a.deleteArtwork(a.deleteID)
	}
	if !a.settingsOpen && !a.previewOpen && !a.deleteOpen && !a.batchOpen() && !a.updateRestarting() {
		if c.Shortcut(ui.Cmd, ui.KeyV) {
			a.importImages(a.desktop.PasteImages)
		}
		if c.Shortcut(ui.Cmd, ui.KeyEnter) && a.canGenerate() {
			a.generate()
		}
	}
}

const noticeLifetime = 3 * time.Second

func (a *App) showNotice(message string) {
	a.notice = message
	a.noticeExpiresAt = time.Time{}
}

func (a *App) updateNotice(c *ui.Context) {
	if a.notice == "" {
		a.noticeExpiresAt = time.Time{}
		return
	}
	now := c.Now()
	if a.noticeExpiresAt.IsZero() {
		a.noticeExpiresAt = now.Add(noticeLifetime)
	}
	if !now.Before(a.noticeExpiresAt) {
		a.showNotice("")
		return
	}
	c.After(a.noticeExpiresAt.Sub(now))
}

func (a *App) titlebar(c *ui.Context) {
	ui.Row(c.Key("workspace-header")).Height(40).FillWidth().Background(c.Theme().Background).Children(func() {
		ui.Box(c.Key("titlebar-drag")).Grow(1).FillHeight().DragWindow()
		if runtime.GOOS != "darwin" {
			ui.Row(c).Gap(2).Padding(0, 8).AlignItems(ui.Center).Children(func() {
				iconButton(c, "window-minimize", locale.T("最小化窗口"), "minimize").Size(28, 28).OnClick(func() { a.withWindow(func(w *mygo.Window) { w.Minimize() }) })
				iconButton(c, "window-maximize", locale.T("最大化或还原窗口"), "maximize").Size(28, 28).OnClick(func() {
					a.withWindow(func(w *mygo.Window) {
						if w.IsMaximized() {
							w.Unmaximize()
						} else {
							w.Maximize()
						}
					})
				})
				iconButton(c, "window-close", locale.T("关闭程序"), "close").Size(28, 28).OnClick(func() { a.withWindow(func(w *mygo.Window) { w.Close() }) })
			})
		}
	})
}
func (a *App) withWindow(fn func(*mygo.Window)) {
	a.windowMu.RLock()
	w := a.window
	a.windowMu.RUnlock()
	if w != nil {
		fn(w)
	}
}
func (a *App) messages(c *ui.Context) {
	if a.problem != "" {
		ui.Row(c.Key("error-message")).FillWidth().Gap(8).Padding(12).Margin(16, 0, 0, 0).Background(c.Theme().Surface).Radius(12).Children(func() {
			ui.Text(c, a.problem).Grow(1).TextColor(c.Theme().Danger).Label(locale.T("错误：") + a.problem)
			if iconButton(c, "dismiss-error", locale.T("关闭提示"), "close").Clicked() {
				a.problem = ""
			}
		})
	}
}

func (a *App) reconcileSelection() {
	previousProvider := a.providerID
	if !contains(ratios, a.ratio) {
		a.ratio = "3:4"
	}
	p := a.provider()
	if p == nil {
		for _, candidate := range a.config.Providers {
			if !candidate.Disabled {
				a.providerID = candidate.ID
				p = a.provider()
				break
			}
		}
	}
	if p == nil {
		a.model = ""
		return
	}
	if !contains(p.Models, a.model) {
		a.model = first(p.Models)
	}
	if previousProvider != a.providerID || !contains(p.Resolutions, a.resolution) {
		a.resolution = providerDefaultResolution(p)
	}
}

func providerDefaultResolution(p *domain.Provider) string {
	if p != nil {
		if p.DefaultResolution != "" && contains(p.Resolutions, p.DefaultResolution) {
			return p.DefaultResolution
		}
		if resolution := first(p.Resolutions); resolution != "" {
			return resolution
		}
	}
	return "2k"
}

// provider is the selected provider while it is enabled.
func (a *App) provider() *domain.Provider {
	for i := range a.config.Providers {
		if a.config.Providers[i].ID == a.providerID && !a.config.Providers[i].Disabled {
			return &a.config.Providers[i]
		}
	}
	return nil
}
func first(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func providerLabels(providers []domain.Provider, selected string) ([]string, string, map[string]string) {
	counts := make(map[string]int)
	for _, p := range providers {
		counts[p.Name]++
	}
	labels := make([]string, len(providers))
	ids := make(map[string]string)
	current := ""
	for i, p := range providers {
		label := p.Name
		if counts[p.Name] > 1 {
			label = fmt.Sprintf("%s (%d)", p.Name, i+1)
		}
		labels[i] = label
		ids[label] = p.ID
		if p.ID == selected {
			current = label
		}
	}
	return labels, current, ids
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if value == v {
			return true
		}
	}
	return false
}
func newID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(data[:])
}

func computeSize(ratio, resolution string) string {
	var w, h float64
	if _, err := fmt.Sscanf(ratio, "%f:%f", &w, &h); err != nil || w <= 0 || h <= 0 {
		return "1024x1024"
	}
	target := float64(2048 * 2048)
	if resolution == "4k" {
		target = 4096 * 4096
	}
	round := func(v float64) int { return max(256, int(math.Floor(v/16+0.5))*16) }
	long := round(math.Sqrt(target * math.Max(w, h) / math.Min(w, h)))
	short := round(float64(long) * math.Min(w, h) / math.Max(w, h))
	if w >= h {
		return fmt.Sprintf("%dx%d", long, short)
	}
	return fmt.Sprintf("%dx%d", short, long)
}

func referenceBytes(ref domain.Reference) ([]byte, error) {
	_, value, ok := strings.Cut(ref.DataURL, ",")
	if !ok {
		return nil, errors.New(locale.T("参考图数据无效。"))
	}
	return base64.StdEncoding.DecodeString(value)
}
