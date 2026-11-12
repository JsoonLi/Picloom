// Package generation preserves Image2AI's OpenAI-compatible image contracts.
package generation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image2ai/internal/locale"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"

	"image2ai/internal/domain"
	"image2ai/internal/network"
)

type Service struct {
	store   domain.Store
	Timeout time.Duration
}

func New(store domain.Store) *Service { return &Service{store: store, Timeout: 5 * time.Minute} }
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(locale.T("系统随机数不可用"))
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func validHTTP(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New(locale.T("服务地址必须是无账号密码、查询参数的 HTTP(S) URL。"))
	}
	return u, nil
}
func NormalizeProviders(input []domain.Provider) ([]domain.Provider, error) {
	if len(input) < 1 || len(input) > 50 {
		return nil, errors.New(locale.T("请保留 1 到 50 个提供商。"))
	}
	enabled := 0
	for _, p := range input {
		if !p.Disabled {
			enabled++
		}
	}
	if enabled == 0 {
		return nil, errors.New(locale.T("请至少保留一个启用的提供商。"))
	}
	result := make([]domain.Provider, 0, len(input))
	seen := map[string]bool{}
	for _, p := range input {
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			p.ID = newID()
		}
		if seen[p.ID] || len(utf16.Encode([]rune(p.ID))) > 200 {
			return nil, errors.New(locale.T("提供商 ID 无效或重复。"))
		}
		seen[p.ID] = true
		p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		if _, err := validHTTP(p.BaseURL); err != nil {
			return nil, err
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			p.Name = locale.T("未命名提供商")
		}
		models := make([]string, 0, len(p.Models))
		for _, m := range p.Models {
			m = strings.TrimSpace(m)
			if m != "" && !slices.Contains(models, m) {
				if len(utf16.Encode([]rune(m))) > 200 {
					return nil, errors.New(locale.T("模型 ID 过长。"))
				}
				models = append(models, m)
			}
		}
		if len(models) == 0 {
			return nil, errors.New(locale.T("每个提供商至少需要一个模型。"))
		}
		p.Models = models
		if p.Resolutions == nil {
			p.Resolutions = []string{"2k", "4k"}
		}
		if len(p.Resolutions) == 0 {
			return nil, errors.New(locale.T("每个提供商至少选择一种图片分辨率。"))
		}
		for _, r := range p.Resolutions {
			if r != "2k" && r != "4k" {
				return nil, errors.New(locale.T("图片分辨率只支持 2K 或 4K。"))
			}
		}
		resolutions := []string{}
		for _, r := range []string{"2k", "4k"} {
			if slices.Contains(p.Resolutions, r) {
				resolutions = append(resolutions, r)
			}
		}
		p.Resolutions = resolutions
		if p.DefaultResolution == "" {
			p.DefaultResolution = resolutions[0]
		}
		if !slices.Contains(resolutions, p.DefaultResolution) {
			return nil, errors.New(locale.T("默认分辨率必须是该提供商支持的分辨率。"))
		}
		if p.GenerationsPath == "" {
			p.GenerationsPath = "/images/generations"
		}
		if p.EditsPath == "" {
			p.EditsPath = "/images/edits"
		}
		p.GenerationsPath = strings.TrimSpace(p.GenerationsPath)
		p.EditsPath = strings.TrimSpace(p.EditsPath)
		for _, v := range []string{p.GenerationsPath, p.EditsPath} {
			if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsAny(v, "?#\\\r\n") {
				return nil, errors.New(locale.T("接口路径必须以单个 / 开头，不包含查询参数。"))
			}
			if _, err := url.ParseRequestURI(v); err != nil {
				return nil, errors.New(locale.T("接口路径无效。"))
			}
		}
		result = append(result, p)
	}
	return result, nil
}

func (s *Service) Generate(parent context.Context, args domain.GenerateArgs, onProgress func(domain.Progress)) (record domain.ImageRecord, resultErr error) {
	jobID := args.JobID
	if jobID == "" {
		jobID = newID()
	}
	written := []string{}
	progress := func(n int, stage, status, message string) {
		if onProgress != nil {
			onProgress(domain.Progress{JobID: jobID, Progress: n, Stage: stage, Status: status, Error: message})
		}
	}
	defer func() {
		if resultErr != nil {
			for _, file := range written {
				_ = os.Remove(file)
			}
			if errors.Is(resultErr, context.DeadlineExceeded) || errors.Is(parent.Err(), context.DeadlineExceeded) {
				resultErr = errors.New(locale.T("生图请求超时，请稍后重试。"))
			}
			if errors.Is(resultErr, context.Canceled) {
				resultErr = errors.New(locale.T("生图请求已取消。"))
			}
			progress(100, locale.T("生成失败"), "error", resultErr.Error())
		}
	}()
	p, err := s.prepare(args)
	if err != nil {
		return record, err
	}
	args, config, provider, inputs, mimes := p.args, p.config, p.provider, p.inputs, p.mimes
	progress(8, locale.T("准备请求"), "generating", "")
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	client, err := network.NewClient(config.Proxy, timeout)
	if err != nil {
		return record, err
	}
	defer client.CloseIdleConnections()
	requestClient := *client
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.BaseURL+p.endpoint, bytes.NewReader(p.body))
	if err != nil {
		return record, errors.New(locale.T("生图接口地址无效。"))
	}
	request.Header.Set("Content-Type", p.contentType)
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	progress(24, locale.T("已提交请求"), "generating", "")
	response, err := requestClient.Do(request)
	if err != nil {
		// The transport/header timeout can fire before ctx's timer is observed.
		if errors.Is(err, context.DeadlineExceeded) {
			return record, context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return record, ctx.Err()
		}
		return record, errors.New(locale.T("生图网络请求失败，请检查服务与代理设置。"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		sensitive := append([]string{provider.APIKey, args.Prompt, args.Markup}, args.ReferenceImages...)
		providerErr := providerResponseError(response, sensitive)
		if ctx.Err() != nil {
			return record, ctx.Err()
		}
		return record, providerErr
	}
	progress(54, locale.T("正在生成"), "generating", "")
	raw, err := readLimited(response, MaxImageBytes)
	if err != nil {
		if ctx.Err() != nil {
			return record, ctx.Err()
		}
		return record, err
	}
	var payload any
	if isSVG(raw) || bytes.HasPrefix(raw, []byte("GIF8")) {
		payload = base64.StdEncoding.EncodeToString(raw)
	} else if json.Unmarshal(raw, &payload) != nil {
		return record, errors.New(locale.T("生图接口未返回有效 JSON。"))
	}
	progress(78, locale.T("解析图片"), "generating", "")
	data, err := resolveImage(ctx, payload, client)
	if err != nil {
		if ctx.Err() != nil {
			return record, ctx.Err()
		}
		return record, err
	}
	mime, width, height, err := decodeImage(data)
	if err != nil {
		return record, err
	}
	if args.OutputFormat != "" && imageExtension(mime) != strings.ReplaceAll(args.OutputFormat, "jpeg", "jpg") {
		return record, fmt.Errorf(locale.T("渠道返回 %s，未返回所选的 %s 格式。请检查渠道是否支持该输出格式。"), imageExtension(mime), args.OutputFormat)
	}
	if args.OutputFormat == "gif" {
		progress(84, locale.T("设置动图播放速度"), "generating", "")
		data, err = retimeGIF(data, args.FrameDurationMS)
		if err != nil {
			return record, err
		}
	}
	id := newID()
	base, components := s.store.Root(), []string{"image-gen"}
	if config.ImageDirectory != "" {
		base, components = config.ImageDirectory, nil
	}
	directory, err := safeDirectory(base, append(components, "images")...)
	if err != nil {
		return record, err
	}
	imagePath := filepath.Join(directory, id+"."+imageExtension(mime))
	if err = writeImage(imagePath, data); err != nil {
		return record, err
	}
	written = append(written, imagePath)
	referencePaths := []string{}
	if len(inputs) > 0 {
		dir, e := safeDirectory(base, append(components, "references")...)
		if e != nil {
			return record, e
		}
		for i, input := range inputs {
			file := filepath.Join(dir, fmt.Sprintf("%s-%d.%s", id, i, imageExtension(mimes[i])))
			if e = writeImage(file, input); e != nil {
				return record, e
			}
			written = append(written, file)
			referencePaths = append(referencePaths, file)
		}
	}
	record = domain.ImageRecord{ID: id, Prompt: args.Prompt, ProviderID: provider.ID, ProviderName: provider.Name, Model: args.Model, Size: args.Size, CreatedAt: time.Now().UnixMilli(), ImagePath: imagePath, MIMEType: mime, Width: width, Height: height, ReferenceCount: len(inputs), ReferencePaths: referencePaths, Favorite: false}
	record.BatchID = args.BatchID
	if args.OutputFormat == "gif" {
		record.GIFFrameCount = args.FrameCount
		record.GIFFrameDurationMS = args.FrameDurationMS
	}
	if err = s.store.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		*rows = append([]domain.ImageRecord{record}, (*rows)...)
		return nil
	}); err != nil {
		return record, err
	}
	progress(100, locale.T("已完成"), "completed", "")
	return record, nil
}

// safeDirectory creates components under root, refusing any link that
// leads outside it.
func safeDirectory(root string, components ...string) (string, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf(locale.T("作品保存位置不可用：%s"), root)
	}
	directory := canonical
	for _, component := range components {
		candidate := filepath.Join(directory, component)
		if err = os.Mkdir(candidate, 0700); err != nil && !os.IsExist(err) {
			return "", errors.New(locale.T("无法创建图片目录。"))
		}
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(canonical, real)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return "", errors.New(locale.T("禁止写入外部图片目录。"))
		}
		info, err := os.Stat(real)
		if err != nil || !info.IsDir() {
			return "", errors.New(locale.T("图片目录无效。"))
		}
		directory = real
	}
	return directory, nil
}
func writeImage(file string, data []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New(locale.T("无法保存图片。"))
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(file)
		return errors.New(locale.T("无法保存完整图片。"))
	}
	return nil
}

func resolveImage(ctx context.Context, payload any, client *http.Client) ([]byte, error) {
	candidate := payload
	if object, ok := payload.(map[string]any); ok {
		for _, key := range []string{"data", "images", "output"} {
			if object[key] != nil {
				candidate = object[key]
				break
			}
		}
	}
	if array, ok := candidate.([]any); ok {
		if len(array) == 0 {
			return nil, errors.New(locale.T("生图接口返回中没有可识别的图片。"))
		}
		candidate = array[0]
	}
	if object, ok := candidate.(map[string]any); ok {
		candidate = nil
		for _, key := range []string{"b64_json", "base64", "image_base64", "url", "image_url", "svg"} {
			if object[key] != nil {
				candidate = object[key]
				break
			}
		}
	}
	value, ok := candidate.(string)
	if !ok {
		return nil, errors.New(locale.T("生图接口返回中没有可识别的图片。"))
	}
	if isSVG([]byte(value)) {
		if len(value) > MaxImageBytes {
			return nil, errors.New(locale.T("SVG 文件过大。"))
		}
		return []byte(value), nil
	}
	if strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://") {
		u, e := url.Parse(value)
		if e != nil || u.User != nil || u.Hostname() == "" {
			return nil, errors.New(locale.T("返回图片地址无效。"))
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
		if e != nil {
			return nil, errors.New(locale.T("返回图片地址无效。"))
		}
		response, e := client.Do(req)
		if e != nil {
			return nil, errors.New(locale.T("图片下载失败，请检查网络。"))
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return nil, fmt.Errorf(locale.T("图片下载失败（HTTP %d）。"), response.StatusCode)
		}
		return readLimited(response, MaxImageBytes)
	}
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		header, data, ok := strings.Cut(value, ",")
		mime, _, _ := strings.Cut(strings.ToLower(header), ";")
		if ok && mime == "data:image/svg+xml" {
			if strings.HasSuffix(strings.ToLower(header), ";base64") {
				return decodeBase64(data, MaxImageBytes)
			}
			decoded, err := url.PathUnescape(data)
			if err != nil || len(decoded) > 4<<20 {
				return nil, errors.New(locale.T("SVG 数据无效或过大。"))
			}
			return []byte(decoded), nil
		}
		if ok && (strings.EqualFold(header, "data:image/gif;base64") || strings.EqualFold(header, "data:image/svg+xml;base64")) {
			return decodeBase64(data, MaxImageBytes)
		}
		return dataURL(value, MaxImageBytes)
	}
	return decodeBase64(value, MaxImageBytes)
}

// DataURL converts validated encoded image bytes for UI references.
func DataURL(data []byte, mime string) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

var imageModelPattern = regexp.MustCompile(`(?i)image|dall[-_]?e|flux|stable[-_]?diffusion|sdxl|sd3|imagen|ideogram|recraft|seedream|qwen[-_]?image|wan.*image`)
