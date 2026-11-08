package studio

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
)

type testBackend struct {
	domain.Backend
	mu              sync.Mutex
	config          domain.Config
	records         []domain.ImageRecord
	image           []byte
	images          map[string][]byte
	fail            error
	generateStarted chan domain.GenerateArgs
	generateRelease chan struct{}
	deleteCalls     int
	thumbReads      int
}

func fixtureBackend() *testBackend {
	img := image.NewRGBA(image.Rect(0, 0, 48, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 48; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 4), uint8(y * 7), 80, 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return &testBackend{image: b.Bytes(), config: domain.Config{Providers: []domain.Provider{{ID: "local", Name: "本机模拟服务", BaseURL: "http://127.0.0.1", Models: []string{"studio-image"}, Resolutions: []string{"2k", "4k"}, GenerationsPath: "/images/generations", EditsPath: "/images/edits"}}, Proxy: domain.ProxyConfig{Mode: "direct"}, Theme: "system", GenerationPreferences: domain.GenerationPreferences{ProviderID: "local", Model: "studio-image", Ratio: "3:4", Resolution: "2k"}}}
}
func (b *testBackend) LoadConfig() (domain.Config, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.config, b.fail
}
func (b *testBackend) ListHistory(q domain.HistoryQuery) (domain.HistoryPage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	favorites := 0
	for _, record := range b.records {
		if record.Favorite {
			favorites++
		}
	}
	return domain.HistoryPage{Items: append([]domain.ImageRecord(nil), b.records...), Page: 1, PageSize: 12, TotalPages: 1, Total: len(b.records), FavoriteCount: favorites}, b.fail
}
func (b *testBackend) DiscoverPricing(context.Context, string) ([]domain.ModelPricing, error) {
	return []domain.ModelPricing{{Model: "studio-image", Currency: "USD", Prices: map[string]float64{"2k": 0.07, "4k": 0.13}}}, nil
}
func (b *testBackend) SavePreferences(p domain.GenerationPreferences) (domain.Config, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.GenerationPreferences = p
	return b.config, nil
}
func (b *testBackend) SaveProviders(p []domain.Provider) (domain.Config, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.Providers = cloneProviders(p)
	return b.config, nil
}
func (b *testBackend) SaveProxy(p domain.ProxyConfig) (domain.Config, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.Proxy = p
	return b.config, nil
}
func (b *testBackend) SaveTheme(s string) (domain.Config, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.Theme = s
	return b.config, nil
}
func (b *testBackend) DiscoverModels(context.Context, domain.Provider) ([]string, error) {
	return []string{"studio-image", "studio-image-hd"}, nil
}
func (b *testBackend) ReadImage(id string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if data := b.images[id]; data != nil {
		return data, nil
	}
	return b.image, nil
}
func (b *testBackend) ReadThumbnail(id string, _, _ int) ([]byte, error) {
	b.mu.Lock()
	b.thumbReads++
	b.mu.Unlock()
	return b.ReadImage(id)
}
func (b *testBackend) ReadReference(string, int) ([]byte, error) { return b.image, nil }
func (b *testBackend) Generate(ctx context.Context, args domain.GenerateArgs, progress func(domain.Progress)) (domain.ImageRecord, error) {
	if b.generateStarted != nil {
		b.generateStarted <- args
	}
	progress(domain.Progress{JobID: args.JobID, Status: "generating", Stage: "正在生成", Progress: 54})
	if b.generateRelease != nil {
		select {
		case <-b.generateRelease:
		case <-ctx.Done():
			return domain.ImageRecord{}, ctx.Err()
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fail != nil {
		return domain.ImageRecord{}, b.fail
	}
	record := domain.ImageRecord{ID: args.JobID, Prompt: args.Prompt, Model: args.Model, ProviderID: args.ProviderID, Size: args.Size, Width: 48, Height: 32}
	b.records = append(b.records, record)
	return record, nil
}
func (b *testBackend) Favorite(id string, value bool) (domain.ImageRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.records {
		if b.records[i].ID == id {
			b.records[i].Favorite = value
			return b.records[i], nil
		}
	}
	return domain.ImageRecord{}, errors.New("作品不存在")
}
func (b *testBackend) Delete(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleteCalls++
	for i := range b.records {
		if b.records[i].ID == id {
			b.records = append(b.records[:i], b.records[i+1:]...)
			return nil
		}
	}
	return errors.New("作品不存在")
}

type testDesktop struct {
	domain.Desktop
	updatesMu    sync.Mutex
	updates      domain.UpdateStatus
	updateChecks int
	refs         []domain.Reference
	exportSaved  bool
	copied       string
	theme        string
}

func (d *testDesktop) DataDir() string { return "isolated-test-data" }
func (d *testDesktop) UpdateStatus() domain.UpdateStatus {
	d.updatesMu.Lock()
	defer d.updatesMu.Unlock()
	return d.updates
}
func (d *testDesktop) CheckForUpdates() error {
	d.updatesMu.Lock()
	defer d.updatesMu.Unlock()
	d.updateChecks++
	return nil
}
func (d *testDesktop) SetUpdatePreferences(checks, downloads bool) (domain.UpdateStatus, error) {
	d.updatesMu.Lock()
	defer d.updatesMu.Unlock()
	d.updates.AutomaticChecks, d.updates.AutomaticDownloads = checks, downloads
	return d.updates, nil
}
func (d *testDesktop) OpenImages(int) ([]domain.Reference, error)            { return d.refs, nil }
func (d *testDesktop) PasteImages(int) ([]domain.Reference, error)           { return d.refs, nil }
func (d *testDesktop) ImportFiles([]string, int) ([]domain.Reference, error) { return d.refs, nil }
func (d *testDesktop) CopyImage(string) error                                { return nil }
func (d *testDesktop) CopyText(text string) error                            { d.copied = text; return nil }
func (d *testDesktop) ExportImage(string) (bool, error)                      { return d.exportSaved, nil }
func (d *testDesktop) SetTheme(theme string)                                 { d.theme = theme }

func settle(a *App, tt *ui.Tester) {
	for i := 0; i < 5; i++ {
		a.workers.Wait()
		a.drain()
		tt.Frame()
	}
}
func fixture(t *testing.T, width, height int) (*App, *ui.Tester, *testBackend, *testDesktop) {
	t.Helper()
	backend := fixtureBackend()
	desktop := &testDesktop{}
	a := New(backend, desktop)
	t.Cleanup(a.Close)
	tt := ui.NewTester(a.View, width, height)
	settle(a, tt)
	return a, tt, backend, desktop
}
func click(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	if err := tt.Click(text); err != nil {
		t.Fatal(err)
	}
}

func TestDraftAndRunningJobSurviveSettings(t *testing.T) {
	a, tt, b, d := fixture(t, 1280, 900)
	d.refs = []domain.Reference{{ID: "reference", Name: "参考图.png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.image)}}
	click(t, tt, "提示词")
	tt.Type("雨后的上海街道")
	click(t, tt, "添加参考图")
	settle(a, tt)
	b.generateStarted = make(chan domain.GenerateArgs, 1)
	b.generateRelease = make(chan struct{})
	click(t, tt, "开始生成")
	args := <-b.generateStarted
	if args.Prompt != "雨后的上海街道" || len(args.ReferenceImages) != 1 {
		t.Fatalf("request lost draft: %+v", args)
	}
	click(t, tt, "提示词")
	tt.Type("下一张的草稿")
	click(t, tt, "打开设置")
	click(t, tt, "关闭设置")
	if a.prompt != "下一张的草稿" || len(a.references) != 0 || len(a.jobs) != 1 {
		t.Fatal("settings reset draft, references or running job")
	}
	close(b.generateRelease)
	settle(a, tt)
	if len(a.jobs) != 0 || len(a.history.Items) != 1 {
		t.Fatal("completed job did not become persisted artwork")
	}
}

func TestLayoutThemesAndChineseComposition(t *testing.T) {
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}} {
		t.Run(fmtSize(size), func(t *testing.T) {
			a, tt, _, _ := fixture(t, size[0], size[1])
			for _, dark := range []bool{false, true} {
				tt.SetDark(dark)
				for _, label := range []string{"提示词", "模型", "画面比例", "分辨率", "添加参考图", "打开设置"} {
					r, ok := tt.Find(label)
					if !ok {
						t.Fatalf("missing %s", label)
					}
					if r.X < 0 || r.X+r.W > float32(size[0])+1 {
						t.Fatalf("%s overflows: %+v", label, r)
					}
				}
				click(t, tt, "提示词")
				tt.Type("中文输入测试")
				if !containsText(a.prompt, "中文输入测试") {
					t.Fatal("Chinese input was lost")
				}
				click(t, tt, "打开设置")
				if r, ok := tt.Find("Base URL"); !ok || r.X+r.W > float32(size[0])+1 {
					t.Fatalf("settings overflow: %+v", r)
				}
				tt.Key(0, ui.KeyEscape)
				if a.settingsOpen {
					t.Fatal("Escape did not dismiss settings")
				}
			}
		})
	}
}

func TestDeleteRequiresConfirmationAndExportCancelIsQuiet(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 1000)
	record := domain.ImageRecord{ID: "artwork", Prompt: "黄昏的海边", Model: "studio-image", Size: "48x32"}
	b.mu.Lock()
	b.records = []domain.ImageRecord{record}
	b.mu.Unlock()
	a.refreshHistory()
	settle(a, tt)
	a.deleteID = record.ID
	a.deleteOpen = true
	tt.Frame()
	click(t, tt, "取消")
	settle(a, tt)
	if b.deleteCalls != 0 {
		t.Fatal("cancel deleted artwork")
	}
	a.openArtwork(record)
	settle(a, tt)
	a.notice = ""
	click(t, tt, "导出原图")
	settle(a, tt)
	if a.notice != "" {
		t.Fatalf("canceled export reported success: %s", a.notice)
	}
	click(t, tt, "关闭")
	a.deleteOpen = true
	tt.Frame()
	click(t, tt, "删除作品")
	settle(a, tt)
	if b.deleteCalls != 1 || len(a.history.Items) != 0 {
		t.Fatal("confirmed deletion did not remove artwork")
	}
}

func TestImportLimitAndProviderResolutionFallback(t *testing.T) {
	a, tt, b, d := fixture(t, 1280, 900)
	for i := 0; i < 5; i++ {
		d.refs = append(d.refs, domain.Reference{Name: "参考图.png", DataURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.image)})
	}
	click(t, tt, "添加参考图")
	settle(a, tt)
	if len(a.references) != 4 || a.problem == "" {
		t.Fatal("reference count limit was not enforced")
	}
	a.config.Providers[0].Resolutions = []string{"4k"}
	a.resolution = "2k"
	a.reconcileSelection()
	if a.resolution != "4k" {
		t.Fatal("unsupported resolution was retained")
	}
}

func TestFailedTaskRetainsRetryParameters(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 900)
	a.prompt = "石桥边的竹林"
	b.fail = errors.New("模拟服务暂不可用")
	a.generate()
	settle(a, tt)
	if len(a.jobs) != 1 || a.jobs[0].Progress.Status != "error" {
		t.Fatal("failed task disappeared")
	}
	args := a.jobs[0].Args
	if args.Prompt != "石桥边的竹林" || args.Model != a.model {
		t.Fatal("retry parameters were lost")
	}
	b.fail = nil
	a.prompt = "重试期间的草稿"
	task := a.jobs[0]
	a.removeJob(task)
	args.JobID = newID()
	a.startJob(args, task.ProviderName)
	settle(a, tt)
	if len(a.history.Items) != 1 || len(a.jobs) != 0 || a.prompt != "重试期间的草稿" {
		t.Fatal("retry did not complete")
	}
}

func TestSettingsDraftsAreIsolatedAndSavedResolutionReconciles(t *testing.T) {
	a, tt, b, _ := fixture(t, 1280, 1000)
	a.prompt = "保留的创作草稿"
	a.resolution = "4k"
	a.openSettings()
	a.draftProviders[0].Name = "尚未保存的名称"
	a.draftProviders[0].Models[0] = "unsaved-image"
	a.settingsOpen = false
	if a.config.Providers[0].Name != "本机模拟服务" || a.config.Providers[0].Models[0] != "studio-image" {
		t.Fatal("cancel mutated saved provider configuration")
	}
	a.openSettings()
	a.draftProviders[0].Resolutions = []string{"2k"}
	a.saveProviders()
	settle(a, tt)
	if a.resolution != "2k" || a.prompt != "保留的创作草稿" {
		t.Fatal("saving provider reset draft or retained unsupported resolution")
	}
	b.mu.Lock()
	saved := b.config.Providers[0].Resolutions
	b.mu.Unlock()
	if len(saved) != 1 || saved[0] != "2k" {
		t.Fatal("resolution not saved")
	}
}

func TestReuseRestoresReferenceImagesAndPreviewCanZoom(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 1000)
	record := domain.ImageRecord{ID: "saved", Prompt: "清晨的雪山", ProviderID: "local", Model: "studio-image", Size: "3552x4736", ReferenceCount: 1}
	a.reuse(record)
	settle(a, tt)
	if a.prompt != record.Prompt || len(a.references) != 1 || a.resolution != "4k" || a.ratio != "3:4" {
		t.Fatal("remix lost source parameters")
	}
	a.openArtwork(record)
	settle(a, tt)
	click(t, tt, "放大")
	if a.viewer.scale <= 1 || a.viewer.fitted {
		t.Fatal("preview did not zoom")
	}
	click(t, tt, "适应窗口")
	if !a.viewer.fitted {
		t.Fatal("fit button did not restore view")
	}
}

func TestDuplicateProviderNamesRemainIndividuallySelectable(t *testing.T) {
	providers := []domain.Provider{{ID: "first", Name: "同名服务"}, {ID: "second", Name: "同名服务"}}
	labels, current, ids := providerLabels(providers, "second")
	if len(labels) != 2 || labels[0] == labels[1] || ids[current] != "second" {
		t.Fatal("duplicate provider names collapsed identity")
	}
}

func TestChineseIMECompositionCommitsWithoutSubmitting(t *testing.T) {
	a, tt, _, _ := fixture(t, 1280, 900)
	click(t, tt, "提示词")
	tt.Compose("zhongwen", 8)
	tt.Compose("中文", 2)
	if a.prompt != "" || len(a.jobs) != 0 {
		t.Fatal("preedit was committed or submitted prematurely")
	}
	tt.Type("中文")
	if a.prompt != "中文" || len(a.jobs) != 0 {
		t.Fatal("IME commit lost text or submitted generation")
	}
	click(t, tt, "打开设置")
	click(t, tt, "提供商名称")
	tt.Compose("新名称", 3)
	// The OS input method consumes its cancel key and sends an empty preedit.
	tt.Compose("", 0)
	if !a.settingsOpen {
		t.Fatal("canceling composition dismissed settings")
	}
	tt.Key(0, ui.KeyEscape)
	if a.settingsOpen {
		t.Fatal("Escape after composition did not dismiss settings")
	}
}

func containsText(text, part string) bool {
	return len(text) >= len(part) && bytes.Contains([]byte(text), []byte(part))
}
func fmtSize(size [2]int) string { return strconv.Itoa(size[0]) + "x" + strconv.Itoa(size[1]) }

func TestNativeScreenshots(t *testing.T) {
	output := os.Getenv("IMAGE2AI_STUDIO_SNAPSHOTS")
	if output == "" {
		t.Skip("set IMAGE2AI_STUDIO_SNAPSHOTS to capture rendered UI")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{1280, 900}, {740, 560}, {375, 812}} {
		for _, dark := range []bool{false, true} {
			a, tt, _, _ := fixture(t, size[0], size[1])
			tt.SetDark(dark)
			theme := "light"
			if dark {
				theme = "dark"
			}
			save := func(name string) {
				t.Helper()
				file, err := os.Create(filepath.Join(output, name+"-"+theme+"-"+fmtSize(size)+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				if err := png.Encode(file, tt.Image()); err != nil {
					t.Fatal(err)
				}
			}
			save("studio")
			seedVisualGallery(a, tt, backendOf(a))
			save("gallery")
			click(t, tt, "提示词")
			tt.Frame()
			save("gallery-focused")
			click(t, tt, "搜索作品")
			tt.Frame()
			a.jobs = []*job{{Args: domain.GenerateArgs{JobID: "visual-generating", Prompt: "晨光里的山间小屋", Model: "studio-image"}, Progress: domain.Progress{Status: "generating", Progress: 54, Stage: "正在生成"}}, {Args: domain.GenerateArgs{JobID: "visual-failed", Prompt: "远山的另一种构图", Model: "studio-image"}, Progress: domain.Progress{Status: "error", Error: "模拟服务暂不可用，请稍后重试。", Stage: "生成失败"}}}
			tt.Frame()
			save("tasks")
			a.jobs = nil
			a.openSettings()
			tt.Frame()
			save("settings")
			click(t, tt, "网络")
			save("settings-network")
			a.draftProxy.Mode = "manual"
			tt.Frame()
			save("settings-proxy")
			click(t, tt, "外观")
			save("settings-appearance")
			click(t, tt, "提供商")
			a.settingsAdvanced = true
			tt.Frame()
			save("settings-advanced")
			a.settingsOpen = false
			a.openArtwork(a.history.Items[0])
			settle(a, tt)
			save("preview")
		}
	}
}
