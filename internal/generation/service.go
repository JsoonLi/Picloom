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
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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
		panic("系统随机数不可用")
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func validHTTP(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("服务地址必须是无账号密码、查询参数的 HTTP(S) URL。")
	}
	return u, nil
}
func NormalizeProviders(input []domain.Provider) ([]domain.Provider, error) {
	if len(input) < 1 || len(input) > 50 {
		return nil, errors.New("请保留 1 到 50 个提供商。")
	}
	result := make([]domain.Provider, 0, len(input))
	seen := map[string]bool{}
	for _, p := range input {
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" {
			p.ID = newID()
		}
		if seen[p.ID] || len(utf16.Encode([]rune(p.ID))) > 200 {
			return nil, errors.New("提供商 ID 无效或重复。")
		}
		seen[p.ID] = true
		p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		if _, err := validHTTP(p.BaseURL); err != nil {
			return nil, err
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			p.Name = "未命名提供商"
		}
		models := make([]string, 0, len(p.Models))
		for _, m := range p.Models {
			m = strings.TrimSpace(m)
			if m != "" && !slices.Contains(models, m) {
				if len(utf16.Encode([]rune(m))) > 200 {
					return nil, errors.New("模型 ID 过长。")
				}
				models = append(models, m)
			}
		}
		if len(models) == 0 {
			return nil, errors.New("每个提供商至少需要一个模型。")
		}
		p.Models = models
		if p.Resolutions == nil {
			p.Resolutions = []string{"2k", "4k"}
		}
		if len(p.Resolutions) == 0 {
			return nil, errors.New("每个提供商至少选择一种图片分辨率。")
		}
		for _, r := range p.Resolutions {
			if r != "2k" && r != "4k" {
				return nil, errors.New("图片分辨率只支持 2K 或 4K。")
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
			return nil, errors.New("默认分辨率必须是该提供商支持的分辨率。")
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
				return nil, errors.New("接口路径必须以单个 / 开头，不包含查询参数。")
			}
			if _, err := url.ParseRequestURI(v); err != nil {
				return nil, errors.New("接口路径无效。")
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
				resultErr = errors.New("生图请求超时，请稍后重试。")
			}
			if errors.Is(resultErr, context.Canceled) {
				resultErr = errors.New("生图请求已取消。")
			}
			progress(100, "生成失败", "error", resultErr.Error())
		}
	}()
	args.Prompt = strings.TrimSpace(args.Prompt)
	if !ValidOutputFormat(args.OutputFormat) {
		return record, errors.New("输出格式无效。")
	}
	if args.Markup != "" {
		if len(args.ReferenceImages) != 1 {
			return record, errors.New("标注编辑需要且只能使用一张原图。")
		}
		if args.OutputFormat == "gif" || args.OutputFormat == "svg" {
			return record, errors.New("标注编辑仅支持默认格式、PNG、JPEG 或 WebP 输出。")
		}
	}
	if args.OutputFormat == "gif" {
		if len(args.ReferenceImages) > 0 {
			return record, errors.New("GIF 目前仅支持文生图，请移除参考图后生成。")
		}
		var err error
		args.FrameCount, args.FrameDurationMS, err = NormalizeGIFTiming(args.FrameCount, args.FrameDurationMS)
		if err != nil {
			return record, err
		}
	}
	if args.Prompt == "" || len(utf16.Encode([]rune(args.Prompt))) > 20000 {
		return record, errors.New("请输入 1 到 20000 字的提示词。")
	}
	if len(args.ReferenceImages) > 4 {
		return record, errors.New("最多添加 4 张参考图。")
	}
	inputs := make([][]byte, 0, len(args.ReferenceImages))
	mimes := []string{}
	for _, value := range args.ReferenceImages {
		data, mime, err := ValidateReference(value)
		if err != nil {
			return record, fmt.Errorf("参考图无效：%w", err)
		}
		inputs = append(inputs, data)
		mimes = append(mimes, mime)
	}
	var markup []byte
	if args.Markup != "" {
		if mimes[0] != "image/png" && mimes[0] != "image/jpeg" && mimes[0] != "image/webp" {
			return record, errors.New("标注编辑的原图必须是 PNG、JPEG 或 WebP。")
		}
		var err error
		markup, err = validateMarkup(args.Markup, inputs[0])
		if err != nil {
			return record, err
		}
	}
	config, err := s.store.ReadConfig()
	if err != nil {
		return record, err
	}
	providers, err := NormalizeProviders(config.Providers)
	if err != nil {
		return record, err
	}
	var provider domain.Provider
	for _, p := range providers {
		if p.ID == args.ProviderID {
			provider = p
			break
		}
	}
	if provider.ID == "" || !slices.Contains(provider.Models, args.Model) {
		return record, errors.New("请选择已配置的提供商和模型。")
	}
	if args.Size == "" {
		args.Size = "1024x1024"
	}
	parts := strings.Split(args.Size, "x")
	autoSize := args.Size == "auto" && len(markup) > 0
	if !autoSize && len(parts) != 2 {
		return record, errors.New("图片尺寸无效。")
	}
	if autoSize {
		parts = nil
	}
	for _, part := range parts {
		n, e := strconv.Atoi(part)
		if e != nil || len(part) < 3 || len(part) > 4 || n < 256 || n > 8192 || n%16 != 0 {
			return record, errors.New("图片尺寸无效。")
		}
	}
	progress(8, "准备请求", "generating", "")
	if args.OutputFormat == "gif" {
		if err := ValidateGIFSettings(args.Size, args.FrameCount, args.FrameDurationMS); err != nil {
			return record, err
		}
	}
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
	var body io.Reader
	contentType := "application/json"
	endpoint := provider.GenerationsPath
	if len(inputs) == 0 {
		fields := map[string]any{"model": args.Model, "prompt": args.Prompt, "n": 1, "size": args.Size, "response_format": "b64_json"}
		if args.OutputFormat == "gif" {
			delete(fields, "response_format")
			fields["frame_count"] = args.FrameCount
			fields["frame_duration_ms"] = args.FrameDurationMS
		}
		if args.OutputFormat != "" {
			fields["output_format"] = args.OutputFormat
		}
		data, _ := json.Marshal(fields)
		body = bytes.NewReader(data)
	} else {
		endpoint = provider.EditsPath
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		if args.OutputFormat != "" {
			if err = writer.WriteField("output_format", args.OutputFormat); err != nil {
				return record, err
			}
		}
		for _, field := range [][2]string{{"model", args.Model}, {"prompt", args.Prompt}, {"size", args.Size}, {"n", "1"}, {"response_format", "b64_json"}} {
			if err = writer.WriteField(field[0], field[1]); err != nil {
				return record, errors.New("无法准备参考图请求。")
			}
		}
		for i, data := range inputs {
			name := "image"
			if i > 0 {
				name = "image[]"
			}
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", fmt.Sprintf("form-data; name=%q; filename=%q", name, fmt.Sprintf("reference-%d.%s", i+1, imageExtension(mimes[i]))))
			header.Set("Content-Type", mimes[i])
			part, e := writer.CreatePart(header)
			if e != nil {
				return record, errors.New("无法准备参考图请求。")
			}
			if _, e = part.Write(data); e != nil {
				return record, errors.New("无法准备参考图请求。")
			}
		}
		if len(markup) > 0 {
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", `form-data; name="markup"; filename="markup.png"`)
			header.Set("Content-Type", "image/png")
			part, e := writer.CreatePart(header)
			if e != nil {
				return record, errors.New("无法准备标注编辑请求。")
			}
			if _, e = part.Write(markup); e != nil {
				return record, errors.New("无法准备标注编辑请求。")
			}
		}
		if err = writer.Close(); err != nil {
			return record, err
		}
		contentType = writer.FormDataContentType()
		body = &buffer
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.BaseURL+endpoint, body)
	if err != nil {
		return record, errors.New("生图接口地址无效。")
	}
	request.Header.Set("Content-Type", contentType)
	if key := strings.TrimSpace(provider.APIKey); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	progress(24, "已提交请求", "generating", "")
	response, err := requestClient.Do(request)
	if err != nil {
		// The transport/header timeout can fire before ctx's timer is observed.
		if errors.Is(err, context.DeadlineExceeded) {
			return record, context.DeadlineExceeded
		}
		if ctx.Err() != nil {
			return record, ctx.Err()
		}
		return record, errors.New("生图网络请求失败，请检查服务与代理设置。")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		sensitive := append([]string{provider.APIKey, args.Prompt, args.Markup}, args.ReferenceImages...)
		providerErr := providerResponseError(response, sensitive)
		if ctx.Err() != nil {
			return record, ctx.Err()
		}
		return record, providerErr
	}
	progress(54, "正在生成", "generating", "")
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
		return record, errors.New("生图接口未返回有效 JSON。")
	}
	progress(78, "解析图片", "generating", "")
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
		return record, fmt.Errorf("渠道返回 %s，未返回所选的 %s 格式。请检查渠道是否支持该输出格式。", imageExtension(mime), args.OutputFormat)
	}
	if args.OutputFormat == "gif" {
		progress(84, "设置动图播放速度", "generating", "")
		data, err = retimeGIF(data, args.FrameDurationMS)
		if err != nil {
			return record, err
		}
	}
	id := newID()
	directory, err := safeDirectory(s.store.Root(), "images")
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
		dir, e := safeDirectory(s.store.Root(), "references")
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
	progress(100, "已完成", "completed", "")
	return record, nil
}

func safeDirectory(root, bucket string) (string, error) {
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", errors.New("用户数据目录不可用。")
	}
	directory := canonical
	for _, component := range []string{"image-gen", bucket} {
		candidate := filepath.Join(directory, component)
		if err = os.Mkdir(candidate, 0700); err != nil && !os.IsExist(err) {
			return "", errors.New("无法创建图片目录。")
		}
		real, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(canonical, real)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return "", errors.New("禁止写入外部图片目录。")
		}
		info, err := os.Stat(real)
		if err != nil || !info.IsDir() {
			return "", errors.New("图片目录无效。")
		}
		directory = real
	}
	return directory, nil
}
func writeImage(file string, data []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("无法保存图片。")
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(file)
		return errors.New("无法保存完整图片。")
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
			return nil, errors.New("生图接口返回中没有可识别的图片。")
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
		return nil, errors.New("生图接口返回中没有可识别的图片。")
	}
	if isSVG([]byte(value)) {
		if len(value) > MaxImageBytes {
			return nil, errors.New("SVG 文件过大。")
		}
		return []byte(value), nil
	}
	if strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://") {
		u, e := url.Parse(value)
		if e != nil || u.User != nil || u.Hostname() == "" {
			return nil, errors.New("返回图片地址无效。")
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
		if e != nil {
			return nil, errors.New("返回图片地址无效。")
		}
		response, e := client.Do(req)
		if e != nil {
			return nil, errors.New("图片下载失败，请检查网络。")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			response.Body.Close()
			return nil, fmt.Errorf("图片下载失败（HTTP %d）。", response.StatusCode)
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
				return nil, errors.New("SVG 数据无效或过大。")
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
