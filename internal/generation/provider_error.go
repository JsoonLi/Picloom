package generation

import (
	"encoding/json"
	"fmt"
	"image2ai/internal/locale"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

const maxProviderErrorBytes = 16 * 1024

var (
	errorURL           = regexp.MustCompile(`(?i)(?:https?|file)://[^\s<>"']+`)
	errorAuthorization = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9._~+/=-]+`)
	errorCredential    = regexp.MustCompile(`(?i)\b(?:api[_-]?key|authorization|access[_-]?token|token|secret|password)\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	errorInput         = regexp.MustCompile(`(?is)\b(?:prompt|reference_images?|image_url|image_base64|b64_json)\b\s*[:=].*`)
	errorDataURL       = regexp.MustCompile(`(?i)data:[^\s<>"']+`)
	errorEncodedData   = regexp.MustCompile(`[a-zA-Z0-9+/]{128,}={0,2}`)
	errorIdentifier    = regexp.MustCompile(`^[a-zA-Z0-9_.:/-]{1,64}$`)
)

// Only bounded structured error fields can reach the UI. Raw responses,
// HTML gateway pages, request echoes and credentials are never logged.
func providerResponseError(response *http.Response, sensitive []string) error {
	fallback := fmt.Errorf(locale.T("生图请求失败（HTTP %d）。渠道未返回可显示的错误详情。"), response.StatusCode)
	data, err := readLimited(response, maxProviderErrorBytes)
	if err != nil {
		return fallback
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(data, &envelope) != nil {
		return fallback
	}
	fields := envelope
	var message string
	if raw := envelope["error"]; len(raw) > 0 {
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil && nested != nil {
			fields = nested
		} else {
			_ = json.Unmarshal(raw, &message)
		}
	}
	readString := func(name string) string { var value string; _ = json.Unmarshal(fields[name], &value); return value }
	if message == "" {
		message = readString("message")
	}
	message = safeProviderErrorText(message, sensitive, 360)
	code := safeProviderErrorText(readString("code"), sensitive, 64)
	parameter := safeProviderErrorText(readString("param"), sensitive, 64)
	identifiers := []string{}
	if errorIdentifier.MatchString(parameter) {
		identifiers = append(identifiers, locale.T("参数 ")+parameter)
	}
	if errorIdentifier.MatchString(code) {
		identifiers = append(identifiers, locale.T("代码 ")+code)
	}
	if message == "" && len(identifiers) == 0 {
		return fallback
	}
	detail := ""
	if len(identifiers) > 0 {
		detail = strings.Join(identifiers, "，") + "。"
	}
	if message != "" {
		detail += locale.T("渠道提示：") + message
	}
	return fmt.Errorf(locale.T("生图请求失败（HTTP %d）。%s"), response.StatusCode, detail)
}

func safeProviderErrorText(value string, sensitive []string, limit int) string {
	for _, secret := range sensitive {
		if secret == "" {
			continue
		}
		value = strings.ReplaceAll(value, secret, locale.T("[已隐藏]"))
		if encoded, err := json.Marshal(secret); err == nil && len(encoded) > 2 {
			value = strings.ReplaceAll(value, string(encoded[1:len(encoded)-1]), locale.T("[已隐藏]"))
		}
	}
	value = errorURL.ReplaceAllString(value, locale.T("[链接已隐藏]"))
	value = errorDataURL.ReplaceAllString(value, locale.T("[图片已隐藏]"))
	value = errorAuthorization.ReplaceAllString(value, locale.T("[鉴权已隐藏]"))
	value = errorCredential.ReplaceAllString(value, locale.T("[凭据已隐藏]"))
	value = errorEncodedData.ReplaceAllString(value, locale.T("[数据已隐藏]"))
	value = errorInput.ReplaceAllString(value, locale.T("[输入内容已隐藏]"))
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if strings.ContainsAny(value, "<>") {
		return ""
	}
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit]) + "…"
	}
	return value
}
