package generation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image2ai/internal/locale"
	"mime/multipart"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"image2ai/internal/domain"
)

// prepared is a validated request, ready to send, and what Generate needs
// to save its result.
type prepared struct {
	args        domain.GenerateArgs
	config      domain.Config
	provider    domain.Provider
	inputs      [][]byte
	mimes       []string
	endpoint    string
	contentType string
	body        []byte
	plan        domain.RequestPlan
}

// Plan validates args as Generate does and returns the request it would
// send, without sending it. The API key is masked and images summarised.
func (s *Service) Plan(args domain.GenerateArgs) (domain.RequestPlan, error) {
	p, err := s.prepare(args)
	if err != nil {
		return domain.RequestPlan{}, err
	}
	return p.plan, nil
}

// maskKey keeps the last four characters of a key so it can be recognised.
func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	runes := []rune(key)
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

// prepare is the single place a generation request is validated and built,
// so a plan shown to the user is the request Generate sends.
func (s *Service) prepare(args domain.GenerateArgs) (*prepared, error) {
	args.Prompt = strings.TrimSpace(args.Prompt)
	if !ValidOutputFormat(args.OutputFormat) {
		return nil, errors.New(locale.T("输出格式无效。"))
	}
	if args.Markup != "" {
		if len(args.ReferenceImages) != 1 {
			return nil, errors.New(locale.T("标注编辑需要且只能使用一张原图。"))
		}
		if args.OutputFormat == "gif" || args.OutputFormat == "svg" {
			return nil, errors.New(locale.T("标注编辑仅支持默认格式、PNG、JPEG 或 WebP 输出。"))
		}
	}
	if args.OutputFormat == "gif" {
		var err error
		args.FrameCount, args.FrameDurationMS, err = NormalizeGIFTiming(args.FrameCount, args.FrameDurationMS)
		if err != nil {
			return nil, err
		}
	}
	if args.Prompt == "" || len(utf16.Encode([]rune(args.Prompt))) > 20000 {
		return nil, errors.New(locale.T("请输入 1 到 20000 字的提示词。"))
	}
	if len(args.ReferenceImages) > domain.MaxReferenceImages {
		return nil, fmt.Errorf(locale.T("最多添加 %d 张参考图。"), domain.MaxReferenceImages)
	}
	inputs := make([][]byte, 0, len(args.ReferenceImages))
	mimes := []string{}
	for _, value := range args.ReferenceImages {
		data, mime, err := ValidateReference(value)
		if err != nil {
			return nil, fmt.Errorf(locale.T("参考图无效：%w"), err)
		}
		inputs = append(inputs, data)
		mimes = append(mimes, mime)
	}
	var markup []byte
	if args.Markup != "" {
		if mimes[0] != "image/png" && mimes[0] != "image/jpeg" && mimes[0] != "image/webp" {
			return nil, errors.New(locale.T("标注编辑的原图必须是 PNG、JPEG 或 WebP。"))
		}
		var err error
		markup, err = validateMarkup(args.Markup, inputs[0])
		if err != nil {
			return nil, err
		}
	}
	config, err := s.store.ReadConfig()
	if err != nil {
		return nil, err
	}
	providers, err := NormalizeProviders(config.Providers)
	if err != nil {
		return nil, err
	}
	var provider domain.Provider
	for _, p := range providers {
		if p.ID == args.ProviderID {
			provider = p
			break
		}
	}
	if provider.ID == "" || !slices.Contains(provider.Models, args.Model) {
		return nil, errors.New(locale.T("请选择已配置的提供商和模型。"))
	}
	if provider.Disabled {
		return nil, fmt.Errorf(locale.T("提供商「%s」已停用，请在设置中启用后再生成。"), provider.Name)
	}
	if args.Size == "" {
		args.Size = "1024x1024"
	}
	parts := strings.Split(args.Size, "x")
	autoSize := args.Size == "auto" && len(markup) > 0
	if !autoSize && len(parts) != 2 {
		return nil, errors.New(locale.T("图片尺寸无效。"))
	}
	if autoSize {
		parts = nil
	}
	for _, part := range parts {
		n, e := strconv.Atoi(part)
		if e != nil || len(part) < 3 || len(part) > 4 || n < 256 || n > 8192 || n%16 != 0 {
			return nil, errors.New(locale.T("图片尺寸无效。"))
		}
	}
	if args.OutputFormat == "gif" {
		if err := ValidateGIFSettings(args.Size, args.FrameCount, args.FrameDurationMS); err != nil {
			return nil, err
		}
	}
	result := &prepared{args: args, config: config, provider: provider, inputs: inputs, mimes: mimes, contentType: "application/json", endpoint: provider.GenerationsPath}
	plan := domain.RequestPlan{Method: "POST", APIKey: maskKey(provider.APIKey), Fields: []domain.RequestField{}, Images: []domain.RequestImage{}}
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
		result.body, _ = json.Marshal(fields)
		// json.Marshal sorts keys, so the plan lists them in the order sent.
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			plan.Fields = append(plan.Fields, domain.RequestField{Name: name, Value: fmt.Sprint(fields[name])})
		}
	} else {
		result.endpoint = provider.EditsPath
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		fields := [][2]string{}
		if args.OutputFormat != "" {
			fields = append(fields, [2]string{"output_format", args.OutputFormat})
		}
		fields = append(fields, [2]string{"model", args.Model}, [2]string{"prompt", args.Prompt}, [2]string{"size", args.Size}, [2]string{"n", "1"})
		if args.OutputFormat == "gif" {
			fields = append(fields, [2]string{"frame_count", strconv.Itoa(args.FrameCount)}, [2]string{"frame_duration_ms", strconv.Itoa(args.FrameDurationMS)})
		} else {
			fields = append(fields, [2]string{"response_format", "b64_json"})
		}
		for _, field := range fields {
			if err = writer.WriteField(field[0], field[1]); err != nil {
				return nil, errors.New(locale.T("无法准备参考图请求。"))
			}
			plan.Fields = append(plan.Fields, domain.RequestField{Name: field[0], Value: field[1]})
		}
		for i, data := range inputs {
			name := "image"
			if i > 0 {
				name = "image[]"
			}
			fileName := fmt.Sprintf("reference-%d.%s", i+1, imageExtension(mimes[i]))
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", fmt.Sprintf("form-data; name=%q; filename=%q", name, fileName))
			header.Set("Content-Type", mimes[i])
			part, e := writer.CreatePart(header)
			if e != nil {
				return nil, errors.New(locale.T("无法准备参考图请求。"))
			}
			if _, e = part.Write(data); e != nil {
				return nil, errors.New(locale.T("无法准备参考图请求。"))
			}
			size, _ := ImageSize(data)
			plan.Images = append(plan.Images, domain.RequestImage{Field: name, FileName: fileName, MIMEType: mimes[i], Width: size.X, Height: size.Y, Bytes: len(data)})
		}
		if len(markup) > 0 {
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", `form-data; name="markup"; filename="markup.png"`)
			header.Set("Content-Type", "image/png")
			part, e := writer.CreatePart(header)
			if e != nil {
				return nil, errors.New(locale.T("无法准备标注编辑请求。"))
			}
			if _, e = part.Write(markup); e != nil {
				return nil, errors.New(locale.T("无法准备标注编辑请求。"))
			}
			size, _ := ImageSize(markup)
			plan.Images = append(plan.Images, domain.RequestImage{Field: "markup", FileName: "markup.png", MIMEType: "image/png", Width: size.X, Height: size.Y, Bytes: len(markup)})
		}
		if err = writer.Close(); err != nil {
			return nil, err
		}
		result.contentType = writer.FormDataContentType()
		result.body = buffer.Bytes()
	}
	plan.URL = provider.BaseURL + result.endpoint
	plan.ContentType = result.contentType
	if i := strings.Index(plan.ContentType, ";"); i >= 0 {
		// The multipart boundary is random per request; it is not a parameter.
		plan.ContentType = plan.ContentType[:i]
	}
	result.plan = plan
	return result, nil
}
