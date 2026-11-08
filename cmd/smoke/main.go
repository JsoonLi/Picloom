// Smoke drives the real native app against an isolated local provider.
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"image2ai/internal/domain"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL native smoke:", err)
		os.Exit(1)
	}
}

type client struct {
	conn   net.Conn
	reader *bufio.Reader
	id     int
}

func (c *client) call(method string, params any) (json.RawMessage, error) {
	c.id++
	data, _ := json.Marshal(map[string]any{"id": c.id, "method": method, "params": params})
	_ = c.conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := c.conn.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var result struct {
		Result json.RawMessage
		Error  *struct{ Code, Message string }
	}
	if err = json.Unmarshal(line, &result); err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, fmt.Errorf("%s: %s (%s)", method, result.Error.Message, result.Error.Code)
	}
	return result.Result, nil
}

type node struct {
	ID, Key, Role, Label, Value string
	States                      []string
}

func (n *node) UnmarshalJSON(data []byte) error {
	type plain node
	var value struct {
		Key any
		*plain
	}
	value.plain = (*plain)(n)
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if key, ok := value.Key.(string); ok {
		n.Key = key
	}
	return nil
}
func (c *client) tree() ([]node, error) {
	data, err := c.call("tree", nil)
	if err != nil {
		return nil, err
	}
	var tree struct{ Nodes []node }
	err = json.Unmarshal(data, &tree)
	return tree.Nodes, err
}
func (c *client) wait(description string, predicate func([]node) bool) error {
	deadline := time.Now().Add(25 * time.Second)
	var last []node
	var lastErr error
	for time.Now().Before(deadline) {
		last, lastErr = c.tree()
		if lastErr == nil && predicate(last) {
			return nil
		}
		time.Sleep(40 * time.Millisecond)
	}
	keys := []string{}
	for _, n := range last {
		keys = append(keys, n.Key+":"+n.Label)
	}
	return fmt.Errorf("等待%s超时：%v；界面 %v", description, lastErr, keys)
}
func find(nodes []node, key string) *node {
	for i := range nodes {
		if nodes[i].Key == key {
			return &nodes[i]
		}
	}
	return nil
}
func (c *client) pressLabel(label string) error {
	id := ""
	if err := c.wait(label, func(nodes []node) bool {
		for _, n := range nodes {
			if n.Label == label && (n.Role == "button" || strings.Contains(strings.ToLower(n.Role), "menu")) {
				id = n.ID
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	_, err := c.call("act", map[string]any{"id": id, "action": "press"})
	if err != nil {
		return err
	}
	_, _ = c.call("idle", map[string]any{"timeout": 2000})
	return nil
}
func (c *client) act(key, action, text string) error {
	if err := c.wait(key, func(n []node) bool { return find(n, key) != nil }); err != nil {
		return err
	}
	_, _ = c.call("act", map[string]any{"key": key, "action": "scrollIntoView"})
	_, _ = c.call("idle", map[string]any{"timeout": 2000})
	_, err := c.call("act", map[string]any{"key": key, "action": action, "text": text})
	if err != nil {
		return err
	}
	_, _ = c.call("idle", map[string]any{"timeout": 2000})
	return nil
}
func (c *client) capture(file string) error {
	data, err := c.call("capture", nil)
	if err != nil {
		return err
	}
	var frame struct{ PNG string }
	if err = json.Unmarshal(data, &frame); err != nil {
		return err
	}
	png, err := base64.StdEncoding.DecodeString(frame.PNG)
	if err != nil {
		return err
	}
	return os.WriteFile(file, png, 0600)
}
func run() error {
	app := flag.String("app", ".cache/native-e2e/Picloom.exe", "native executable to verify")
	release := flag.Bool("release", false, "verify release startup and absence of automation")
	out := flag.String("out", "test-results/native", "diagnostic output")
	flag.Parse()
	executable, err := filepath.Abs(*app)
	if err != nil {
		return err
	}
	if _, err = os.Stat(executable); err != nil {
		return err
	}
	if err = os.MkdirAll(*out, 0700); err != nil {
		return err
	}
	root, err := os.MkdirTemp("", "i2a-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	pixels := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			pixels.SetNRGBA(x, y, color.NRGBA{uint8(x * 255 / 640), uint8(y * 255 / 480), 160, 255})
		}
	}
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, pixels)
	picture := encoded.Bytes()
	var requests, edits, failures atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"id":"mock-image","output_modalities":["image"]}]}`)
		case "/v1/models/pricing":
			fmt.Fprint(w, `{"models":[{"id":"mock-image","billing_mode":"per_image","list_cost":{"currency":"USD"},"per_call_tiers":[{"id":"2k","cost":0.02},{"id":"4k","cost":0.04}]}]}`)
		case "/v1/images/generations", "/v1/images/edits":
			requests.Add(1)
			prompt := ""
			if strings.Contains(r.Header.Get("Content-Type"), "multipart") {
				if e := r.ParseMultipartForm(5 << 20); e != nil {
					http.Error(w, "invalid multipart", 400)
					return
				}
				defer r.MultipartForm.RemoveAll()
				prompt = r.FormValue("prompt")
				if len(r.MultipartForm.File["image"]) != 1 {
					http.Error(w, "image missing", 400)
					return
				}
				edits.Add(1)
			} else {
				var args domain.GenerateArgs
				if e := json.NewDecoder(r.Body).Decode(&args); e != nil {
					http.Error(w, "invalid json", 400)
					return
				}
				prompt = args.Prompt
			}
			// These are synthetic local-provider requests only. Keep their prompts
			// in CI output so a failed UI action is distinguishable from a provider error.
			fmt.Printf("MOCK generation request: path=%q prompt=%q\n", r.URL.Path, prompt)
			if strings.Contains(prompt, "fail-once") && failures.Add(1) == 1 {
				http.Error(w, "synthetic failure", 503)
				return
			}
			time.Sleep(550 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(picture)}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := domain.Config{Theme: "system", Proxy: domain.ProxyConfig{Mode: "direct", Bypass: "<local>,localhost,127.0.0.1"}, Providers: []domain.Provider{{ID: "mock", Name: "本地模拟服务", BaseURL: server.URL + "/v1", APIKey: "synthetic-key", Models: []string{"mock-image"}, Resolutions: []string{"2k", "4k"}, GenerationsPath: "/images/generations", EditsPath: "/images/edits"}}, GenerationPreferences: domain.GenerationPreferences{ProviderID: "mock", Model: "mock-image", Ratio: "3:4", Resolution: "2k"}}
	configBytes, _ := json.MarshalIndent(config, "", "  ")
	if err = os.WriteFile(filepath.Join(root, "config.json"), configBytes, 0600); err != nil {
		return err
	}
	images := filepath.Join(root, "image-gen", "images")
	_ = os.MkdirAll(images, 0700)
	legacyPath := filepath.Join(images, "legacy.png")
	_ = os.WriteFile(legacyPath, picture, 0600)
	records := []domain.ImageRecord{{ID: "legacy", Prompt: "旧版合成作品", ProviderID: "mock", ProviderName: "本地模拟服务", Model: "mock-image", Size: "640x480", CreatedAt: time.Now().Add(-time.Hour).UnixMilli(), ImagePath: legacyPath, MIMEType: "image/png", Width: 640, Height: 480, ReferencePaths: []string{}}}
	historyFile := filepath.Join(root, "image-gen", "history.json")
	historyBytes, _ := json.Marshal(records)
	_ = os.WriteFile(historyFile, historyBytes, 0600)
	socket := filepath.Join(root, "test.sock")
	var logs bytes.Buffer
	start := func() (*exec.Cmd, error) {
		cmd := exec.Command(executable)
		cmd.Env = append(os.Environ(), "IMAGE2AI_USER_DATA="+root, "MYGO_AUTOMATION="+socket)
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		return cmd, cmd.Start()
	}
	process, err := start()
	if err != nil {
		return err
	}
	defer func() {
		if process != nil {
			_ = process.Process.Kill()
			_ = process.Wait()
		}
		_ = os.WriteFile(filepath.Join(*out, "app.log"), logs.Bytes(), 0600)
	}()
	if *release {
		if err = waitReleaseWindow(process.Process.Pid); err != nil {
			return fmt.Errorf("%w；%s", err, logs.String())
		}
		if _, err = os.Stat(socket); !os.IsNotExist(err) {
			return errors.New("正式构建意外开放了 automation")
		}
		if err = stopProcess(process); err != nil {
			return err
		}
		process = nil
		fmt.Println("PASS release startup: native window, isolated legacy data, automation absent.")
		return nil
	}
	var conn net.Conn
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("unix", socket, 250*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("无法连接原生测试窗口：%w；%s", err, logs.String())
	}
	c := &client{conn: conn, reader: bufio.NewReader(conn)}
	defer conn.Close()
	windowData, err := c.call("windows", nil)
	if err != nil {
		return err
	}
	var nativeWindows struct{ Windows []struct{ Kind, URL string } }
	if json.Unmarshal(windowData, &nativeWindows) != nil || len(nativeWindows.Windows) == 0 {
		return errors.New("原生窗口未创建")
	}
	for _, w := range nativeWindows.Windows {
		if w.Kind != "content" || w.URL != "" {
			return errors.New("测试程序意外包含 WebView 窗口")
		}
	}
	if err = c.wait("旧作品", func(n []node) bool { return find(n, "preview-legacy") != nil }); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if err = c.act("window-maximize", "press", ""); err != nil {
			return err
		}
		if err = c.wait("原生最大化", func(_ []node) bool { value, e := windowMaximized(process.Process.Pid); return e == nil && value }); err != nil {
			return err
		}
		if err = c.act("window-maximize", "press", ""); err != nil {
			return err
		}
		if err = c.wait("原生还原", func(_ []node) bool { value, e := windowMaximized(process.Process.Pid); return e == nil && !value }); err != nil {
			return err
		}
	}
	backupConfig, e := os.ReadFile(filepath.Join(root, "native-backup-v1", "config.json"))
	if e != nil || !bytes.Equal(backupConfig, configBytes) {
		return errors.New("首次接管没有完整备份旧配置")
	}
	backupHistory, e := os.ReadFile(filepath.Join(root, "native-backup-v1", "history.json"))
	if e != nil || !bytes.Equal(backupHistory, historyBytes) {
		return errors.New("首次接管没有完整备份旧历史")
	}
	if err = c.capture(filepath.Join(*out, "desktop.png")); err != nil {
		return err
	}
	const prompt = "中文原生绘图 smoke 测试"
	for _, step := range [][3]string{{"prompt", "setValue", prompt}, {"generate", "press", ""}} {
		if err = c.act(step[0], step[1], step[2]); err != nil {
			return err
		}
	}
	if err = c.wait("提交后清空提示词", func(n []node) bool { p := find(n, "prompt"); return p != nil && p.Value == "" }); err != nil {
		return err
	}
	const nextDraft = "下一条创作草稿"
	for _, step := range [][3]string{{"prompt", "setValue", nextDraft}, {"open-settings", "press", ""}, {"close-settings", "press", ""}} {
		if err = c.act(step[0], step[1], step[2]); err != nil {
			return err
		}
	}
	if err = c.wait("设置保留下一条草稿", func(n []node) bool { p := find(n, "prompt"); return p != nil && p.Value == nextDraft }); err != nil {
		return err
	}
	lastRecords := records
	loadHistory := func() []domain.ImageRecord {
		var rows []domain.ImageRecord
		data, readErr := os.ReadFile(historyFile)
		if readErr == nil && json.Unmarshal(data, &rows) == nil {
			lastRecords = rows
		}
		return lastRecords
	}
	if err = c.wait("生成完成", func(n []node) bool { return len(loadHistory()) == 2 }); err != nil {
		return err
	}
	id := loadHistory()[0].ID
	if err = c.act("favorite-"+id, "press", ""); err != nil {
		return err
	}
	if err = c.wait("收藏持久化", func(n []node) bool { return loadHistory()[0].Favorite }); err != nil {
		return err
	}
	if err = c.act("preview-"+id, "press", ""); err != nil {
		return err
	}
	if err = c.wait("原图预览", func(n []node) bool { return find(n, "preview-image") != nil }); err != nil {
		return err
	}
	if err = c.capture(filepath.Join(*out, "preview.png")); err != nil {
		return err
	}
	if err = c.act("copy-image", "press", ""); err != nil {
		return err
	}
	if err = c.act("close-preview", "press", ""); err != nil {
		return err
	}
	if err = c.wait("复制图片完成", func(n []node) bool {
		for _, v := range n {
			if strings.Contains(v.Label, "图片已复制") {
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	if _, err = c.call("input", map[string]any{"events": []map[string]string{{"type": "key", "key": "CmdOrCtrl+V"}}}); err != nil {
		return err
	}
	if err = c.wait("粘贴参考图", func(n []node) bool {
		for _, v := range n {
			if strings.HasPrefix(v.Key, "remove-reference-") {
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	if err = c.act("prompt", "setValue", "参考图编辑 smoke 测试"); err != nil {
		return err
	}
	if err = c.act("generate", "press", ""); err != nil {
		return err
	}
	if err = c.wait("参考图编辑", func(n []node) bool { return len(loadHistory()) == 3 }); err != nil {
		return err
	}
	if edits.Load() != 1 {
		return fmt.Errorf("预期一次 multipart 编辑，实际 %d", edits.Load())
	}
	edited := loadHistory()[0]
	if edited.ReferenceCount != 1 {
		return errors.New("未持久化参考图")
	}
	if err = c.act("reuse-"+edited.ID, "press", ""); err != nil {
		return err
	}
	// UI idle does not wait for reference decoding in the reuse worker. Wait
	// for its observable result before entering the next request, otherwise
	// that worker can replace the fail-once prompt with the reused prompt.
	if err = c.wait("复用提示词与参考图完成", func(nodes []node) bool {
		prompt := find(nodes, "prompt")
		if prompt == nil || prompt.Value != edited.Prompt {
			return false
		}
		references := 0
		for _, n := range nodes {
			if strings.HasPrefix(n.Key, "remove-reference-") {
				references++
			}
		}
		return references == edited.ReferenceCount
	}); err != nil {
		return err
	}
	if err = c.act("prompt", "setValue", "fail-once 原生重试"); err != nil {
		return err
	}
	if err = c.act("generate", "press", ""); err != nil {
		return err
	}
	retry := ""
	if err = c.wait("失败重试", func(n []node) bool {
		for _, v := range n {
			if strings.HasPrefix(v.Key, "retry-") {
				retry = v.Key
				return true
			}
		}
		return false
	}); err != nil {
		return err
	}
	if err = c.act(retry, "press", ""); err != nil {
		return err
	}
	if err = c.wait("重试成功", func(n []node) bool { return len(loadHistory()) == 4 }); err != nil {
		return err
	}
	if err = c.act("history-search", "setValue", "旧版合成作品"); err != nil {
		return err
	}
	if err = c.wait("搜索", func(n []node) bool { return find(n, "preview-legacy") != nil && find(n, "preview-"+id) == nil }); err != nil {
		return err
	}
	if err = c.capture(filepath.Join(*out, "search.png")); err != nil {
		return err
	}
	if err = c.act("history-search", "setValue", ""); err != nil {
		return err
	}
	if err = c.wait("全部作品", func(n []node) bool { return find(n, "preview-"+id) != nil }); err != nil {
		return err
	}
	if err = c.act("open-settings", "press", ""); err != nil {
		return err
	}
	if err = c.capture(filepath.Join(*out, "settings.png")); err != nil {
		return err
	}
	if err = c.act("settings-tab-外观", "press", ""); err != nil {
		return err
	}
	if err = c.act("theme", "focus", ""); err != nil {
		return err
	}
	if err = c.act("theme", "press", ""); err != nil {
		return err
	}
	for _, key := range []string{"Home", "Down", "Enter"} {
		if _, err = c.call("input", map[string]any{"events": []map[string]string{{"type": "key", "key": key}}}); err != nil {
			return err
		}
		_, _ = c.call("idle", map[string]any{"timeout": 2000})
	}
	if err = c.act("save-theme", "press", ""); err != nil {
		return err
	}
	if err = c.wait("浅色持久化", func(_ []node) bool {
		var cfg domain.Config
		data, _ := os.ReadFile(filepath.Join(root, "config.json"))
		return json.Unmarshal(data, &cfg) == nil && cfg.Theme == "light"
	}); err != nil {
		return err
	}
	if err = c.act("close-settings", "press", ""); err != nil {
		return err
	}
	if err = c.capture(filepath.Join(*out, "light.png")); err != nil {
		return err
	}
	if err = c.act("delete-legacy", "press", ""); err != nil {
		return err
	}
	if err = c.pressLabel("删除作品"); err != nil {
		return err
	}
	// Removing the history entry precedes asynchronous image cleanup. Wait for
	// both observable results instead of sampling the intermediate state.
	if err = c.wait("删除作品与原图", func(_ []node) bool {
		_, statErr := os.Stat(legacyPath)
		return len(loadHistory()) == 3 && os.IsNotExist(statErr)
	}); err != nil {
		return err
	}
	if err = c.capture(filepath.Join(*out, "completed.png")); err != nil {
		return err
	}
	conn.Close()
	if err = stopProcess(process); err != nil {
		return err
	}
	process = nil
	process, err = start()
	if err != nil {
		return err
	}
	deadline = time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("unix", socket, 250*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("重启连接失败：%w", err)
	}
	defer conn.Close()
	c = &client{conn: conn, reader: bufio.NewReader(conn)}
	if err = c.wait("重启作品与收藏", func(n []node) bool {
		v := find(n, "history-count")
		return v != nil && strings.Contains(v.Label, "3 张作品") && strings.Contains(v.Label, "1 张收藏")
	}); err != nil {
		return err
	}
	if err = c.capture(filepath.Join(*out, "restarted.png")); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		_, _ = c.call("act", map[string]any{"key": "window-close", "action": "press"})
	}
	conn.Close()
	if err = stopProcess(process); err != nil {
		return err
	}
	process = nil
	fmt.Printf("PASS native E2E: legacy data, Chinese prompt, settings retention, generation, clipboard, multipart, persisted references, favorite, search, preview, retry, theme, deletion and restart; %d local requests.\n", requests.Load())
	return nil
}

func stopProcess(cmd *exec.Cmd) error {
	if runtime.GOOS == "windows" {
		script := "$p=Get-Process -Id " + strconv.Itoa(cmd.Process.Pid) + " -ErrorAction SilentlyContinue; if (!$p) { exit 0 }; [void]$p.CloseMainWindow(); if (!$p.WaitForExit(8000)) { exit 1 }"
		if err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Run(); err != nil {
			return errors.New("原生测试程序未正常关闭")
		}
	} else {
		if err := exec.Command("kill", "-TERM", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
			return err
		}
	}
	return cmd.Wait()
}

func waitReleaseWindow(pid int) error {
	if runtime.GOOS == "windows" {
		// Inspect only the exact test process, never search or act on another app.
		script := "$deadline=(Get-Date).AddSeconds(20); do { $p=Get-Process -Id " + strconv.Itoa(pid) + " -ErrorAction SilentlyContinue; if (!$p) { exit 1 }; if ($p.MainWindowHandle -ne 0 -and $p.MainWindowTitle -eq 'Picloom · 绘织') { exit 0 }; Start-Sleep -Milliseconds 100 } while ((Get-Date) -lt $deadline); exit 2"
		return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Run()
	}
	// Native runners have a desktop session; the process must survive startup.
	time.Sleep(3 * time.Second)
	return exec.Command("kill", "-0", strconv.Itoa(pid)).Run()
}
