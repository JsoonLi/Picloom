package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"image2ai/internal/domain"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memoryStore struct {
	mu     sync.Mutex
	root   string
	config domain.Config
	rows   []domain.ImageRecord
	fail   bool
}

func (s *memoryStore) Root() string { return s.root }
func (s *memoryStore) ReadConfig() (domain.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config, nil
}
func (s *memoryStore) UpdateConfig(fn func(*domain.Config) error) (domain.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := fn(&s.config)
	return s.config, err
}
func (s *memoryStore) ReadHistory() ([]domain.ImageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ImageRecord{}, s.rows...), nil
}
func (s *memoryStore) ChangeHistory(fn func(*[]domain.ImageRecord) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("fixture storage failure")
	}
	return fn(&s.rows)
}
func fixturePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
func setupService(t *testing.T, handler http.HandlerFunc) (*Service, *memoryStore, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store := &memoryStore{root: t.TempDir(), config: domain.Config{Providers: []domain.Provider{{ID: "local", Name: "Local fixture", BaseURL: server.URL, APIKey: "local-test-token", Models: []string{"fixture-image"}}}, Proxy: domain.ProxyConfig{Mode: "direct"}}}
	return New(store), store, server
}
func baseArgs() domain.GenerateArgs {
	return domain.GenerateArgs{JobID: "job-1", Prompt: "a quiet studio", ProviderID: "local", Model: "fixture-image", Size: "1024x1024"}
}
func sendPNG(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(fixturePNG())}}})
}

func TestGenerateJSONPersistsBytesAndProgress(t *testing.T) {
	var request map[string]any
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-test-token" {
			t.Error("auth missing")
		}
		if r.URL.Path != "/images/generations" {
			t.Error(r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&request)
		sendPNG(w)
	})
	events := []domain.Progress{}
	record, err := service.Generate(context.Background(), baseArgs(), func(p domain.Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	if request["n"] != float64(1) || request["response_format"] != "b64_json" || request["prompt"] != "a quiet studio" || request["model"] != "fixture-image" || request["size"] != "1024x1024" {
		t.Fatal(request)
	}
	data, _ := os.ReadFile(record.ImagePath)
	if !bytes.Equal(data, fixturePNG()) {
		t.Fatal("bytes changed")
	}
	if record.Width != 4 || record.Height != 3 || record.ReferenceCount != 0 {
		t.Fatal(record)
	}
	rows, _ := store.ReadHistory()
	if len(rows) != 1 || rows[0].ID != record.ID {
		t.Fatal(rows)
	}
	if len(events) != 5 || events[4].Status != "completed" || events[4].JobID != "job-1" {
		t.Fatal(events)
	}
}
func TestGenerateMultipartPersistsReferences(t *testing.T) {
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Error(r.URL.Path)
		}
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			t.Error(err)
		}
		for _, key := range []string{"image", "image[]"} {
			files := r.MultipartForm.File[key]
			if len(files) != 1 || files[0].Header.Get("Content-Type") != "image/png" {
				t.Error(key, files)
			}
		}
		if r.FormValue("n") != "1" || r.FormValue("response_format") != "b64_json" {
			t.Error(r.Form)
		}
		sendPNG(w)
	})
	args := baseArgs()
	args.ReferenceImages = []string{DataURL(fixturePNG(), "image/png"), DataURL(fixturePNG(), "image/png")}
	record, err := service.Generate(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.ReferenceCount != 2 || len(record.ReferencePaths) != 2 {
		t.Fatal(record)
	}
	for _, path := range record.ReferencePaths {
		data, _ := os.ReadFile(path)
		if !bytes.Equal(data, fixturePNG()) {
			t.Fatal(path)
		}
	}
}
func TestImageURLDownloadAndRedirectNeverGetProviderKey(t *testing.T) {
	var calls atomic.Int32
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("leaked provider key")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/image", 302)
			return
		}
		w.Write(fixturePNG())
	}))
	defer imageServer.Close()
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"output": []string{imageServer.URL + "/redirect"}})
	})
	_, err := service.Generate(context.Background(), baseArgs(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
}
func TestProviderRedirectNotFollowed(t *testing.T) {
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++; sendPNG(w) }))
	defer target.Close()
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) })
	_, err := service.Generate(context.Background(), baseArgs(), nil)
	if err == nil || targetCalls != 0 {
		t.Fatal(err, targetCalls)
	}
	rows, _ := store.ReadHistory()
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}
func TestInvalidInputsRejectedBeforeRequest(t *testing.T) {
	calls := 0
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { calls++; sendPNG(w) })
	cases := []domain.GenerateArgs{}
	for _, mutate := range []func(*domain.GenerateArgs){func(a *domain.GenerateArgs) { a.Prompt = "" }, func(a *domain.GenerateArgs) { a.Prompt = strings.Repeat("x", 20001) }, func(a *domain.GenerateArgs) { a.Prompt = strings.Repeat("😀", 10001) }, func(a *domain.GenerateArgs) { a.Model = "unknown" }, func(a *domain.GenerateArgs) { a.Size = "8193x256" }, func(a *domain.GenerateArgs) { a.ReferenceImages = []string{"data:image/png;base64,iVBORw0KGgo="} }, func(a *domain.GenerateArgs) { a.ReferenceImages = make([]string, 5) }} {
		args := baseArgs()
		mutate(&args)
		cases = append(cases, args)
	}
	for _, args := range cases {
		if _, err := service.Generate(context.Background(), args, nil); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
func TestTimeoutMalformedResponseAndFailuresLeaveNoHistory(t *testing.T) {
	for _, scenario := range []string{"timeout", "invalid-json", "invalid-image", "http-error", "oversize"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "timeout":
					io.Copy(io.Discard, r.Body)
					<-r.Context().Done()
				case "invalid-json":
					io.WriteString(w, "no-json")
				case "invalid-image":
					io.WriteString(w, `{"data":[{"b64_json":"YWJj"}]}`)
				case "http-error":
					http.Error(w, "private server details", 503)
				case "oversize":
					w.Header().Set("Content-Length", "50000000")
					w.WriteHeader(200)
				}
			})
			service.Timeout = 30 * time.Millisecond
			events := []domain.Progress{}
			_, err := service.Generate(context.Background(), baseArgs(), func(p domain.Progress) { events = append(events, p) })
			if err == nil {
				t.Fatal("expected error")
			}
			if scenario == "timeout" && !strings.Contains(err.Error(), "超时") {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), "private server") {
				t.Fatal("leaked provider response")
			}
			rows, _ := store.ReadHistory()
			if len(rows) != 0 || events[len(events)-1].Status != "error" {
				t.Fatal(rows, events)
			}
		})
	}
}
func TestHistoryFailureCleansGeneratedFiles(t *testing.T) {
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { sendPNG(w) })
	store.fail = true
	args := baseArgs()
	args.ReferenceImages = []string{DataURL(fixturePNG(), "image/png")}
	if _, err := service.Generate(context.Background(), args, nil); err == nil {
		t.Fatal("expected storage failure")
	}
	for _, bucket := range []string{"images", "references"} {
		entries, _ := os.ReadDir(filepath.Join(store.root, "image-gen", bucket))
		if len(entries) != 0 {
			t.Fatal(entries)
		}
	}
}

func TestOutsideImageDirectoryIsRejectedBeforeCreatingChildren(t *testing.T) {
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { sendPNG(w) })
	outside := t.TempDir()
	link := filepath.Join(store.root, "image-gen")
	err := os.Symlink(outside, link)
	if err != nil && runtime.GOOS == "windows" {
		_, err = exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, outside).CombinedOutput()
	}
	if err != nil {
		t.Skipf("directory links unavailable: %v", err)
	}
	if _, err = service.Generate(context.Background(), baseArgs(), nil); err == nil {
		t.Fatal("accepted external directory")
	}
	if _, err = os.Stat(filepath.Join(outside, "images")); !os.IsNotExist(err) {
		t.Fatal("created a child through an external directory link")
	}
}
func TestConcurrentGenerationRetainsAllResults(t *testing.T) {
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { sendPNG(w) })
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			if _, err := service.Generate(context.Background(), baseArgs(), nil); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	rows, _ := store.ReadHistory()
	if len(rows) != 8 {
		t.Fatal(len(rows))
	}
	ids := map[string]bool{}
	for _, row := range rows {
		if ids[row.ID] {
			t.Fatal("duplicate id")
		}
		ids[row.ID] = true
	}
}
func TestReferenceValidationAndResponseVariants(t *testing.T) {
	data := fixturePNG()
	client := http.DefaultClient
	for _, payload := range []any{map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(data)}}}, map[string]any{"images": []any{DataURL(data, "image/png")}}, map[string]any{"output": map[string]any{"image_base64": base64.RawStdEncoding.EncodeToString(data)}}} {
		result, err := resolveImage(context.Background(), payload, client)
		if err != nil || !bytes.Equal(result, data) {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,!!!", "data:image/png;base64,iVBORw0KGgo="} {
		if _, _, err := ValidateReference(value); err == nil {
			t.Fatal(value)
		}
	}
}
func TestProvidersAndParameters(t *testing.T) {
	p := domain.Provider{ID: "local", BaseURL: "https://example.test/v1/", Models: []string{" image ", "image"}}
	providers, err := NormalizeProviders([]domain.Provider{p})
	if err != nil {
		t.Fatal(err)
	}
	if providers[0].GenerationsPath != "/images/generations" || providers[0].BaseURL != "https://example.test/v1" || len(providers[0].Models) != 1 || len(providers[0].Resolutions) != 2 {
		t.Fatal(providers)
	}
	for _, mutate := range []func(*domain.Provider){
		func(p *domain.Provider) { p.BaseURL = "file:///secret" },
		func(p *domain.Provider) { p.BaseURL = "https://local-test:local-test@example.test" },
		func(p *domain.Provider) { p.BaseURL = "https://example.test?key=x" },
		func(p *domain.Provider) { p.GenerationsPath = "//foreign.test" },
		func(p *domain.Provider) { p.Resolutions = []string{} },
		func(p *domain.Provider) { p.Resolutions = []string{"1k"} },
	} {
		bad := p
		mutate(&bad)
		if _, err := NormalizeProviders([]domain.Provider{bad}); err == nil {
			t.Fatal(bad)
		}
	}
	if ComputeSize("1:1", "2k") != "2048x2048" || ComputeSize("16:9", "1k") != "1360x768" || ComputeSize("invalid", "2k") != "1024x1024" {
		t.Fatal("size parity")
	}
	ratio, resolution := FindSizeCombo("1360x768")
	if ratio != "16:9" || resolution != "2k" {
		t.Fatal(ratio, resolution)
	}
}

func TestProviderDefaultResolution(t *testing.T) {
	for _, tc := range []struct {
		name, selected, want string
		supported            []string
	}{
		{"legacy", "", "2k", nil},
		{"4K only", "", "4k", []string{"4k"}},
		{"prefer 4K", "4k", "4k", []string{"2k", "4k"}},
		{"unsupported", "4k", "", []string{"2k"}},
		{"unknown", "8k", "", []string{"2k", "4k"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers, err := NormalizeProviders([]domain.Provider{{ID: "local", BaseURL: "http://127.0.0.1", Models: []string{"fixture-image"}, Resolutions: tc.supported, DefaultResolution: tc.selected}})
			if tc.want == "" {
				if err == nil {
					t.Fatal("invalid default accepted")
				}
				return
			}
			if err != nil || providers[0].DefaultResolution != tc.want {
				t.Fatalf("default resolution: want %s, error %v", tc.want, err)
			}
		})
	}
}

func TestDisabledProviderRefusesGenerationAndOneStaysEnabled(t *testing.T) {
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { sendPNG(w) })
	store.config.Providers[0].Disabled = true
	if _, err := NormalizeProviders(store.config.Providers); err == nil {
		t.Fatal("saving with every provider disabled must be refused")
	}
	enabled := store.config.Providers[0]
	enabled.ID, enabled.Disabled = "enabled", false
	store.config.Providers = append(store.config.Providers, enabled)
	if _, err := service.Generate(context.Background(), baseArgs(), nil); err == nil || !strings.Contains(err.Error(), "已停用") {
		t.Fatalf("a disabled provider must refuse generation: %v", err)
	}
	args := baseArgs()
	args.ProviderID = "enabled"
	if _, err := service.Generate(context.Background(), args, nil); err != nil {
		t.Fatalf("an enabled provider must still generate: %v", err)
	}
}
