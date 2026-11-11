package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"image2ai/internal/locale"
	"path/filepath"
	"strings"
)

func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var result map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, errors.New(locale.T("需要 JSON 对象"))
	}
	return result, nil
}
func stringField(obj map[string]json.RawMessage, key string) (string, error) {
	raw, ok := obj[key]
	if !ok || string(raw) == "null" {
		return "", fmt.Errorf(locale.T("缺少字符串字段 %s"), key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return value, nil
}
func stringArray(raw json.RawMessage) ([]string, error) {
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, errors.New(locale.T("需要字符串数组"))
	}
	result := make([]string, len(values))
	for i, v := range values {
		if string(v) == "null" || json.Unmarshal(v, &result[i]) != nil {
			return nil, errors.New(locale.T("需要字符串数组"))
		}
	}
	return result, nil
}
func member(value string, allowed ...string) bool {
	for _, v := range allowed {
		if value == v {
			return true
		}
	}
	return false
}
func validateConfig(raw []byte) error {
	obj, err := object(raw)
	if err != nil {
		return err
	}
	if _, ok := obj["imageDirectory"]; ok {
		dir, err := stringField(obj, "imageDirectory")
		if err != nil || (dir != "" && !filepath.IsAbs(dir)) {
			return errors.New(locale.T("作品保存位置无效"))
		}
	}
	if raw, ok := obj["previousImageDirectories"]; ok {
		dirs, err := stringArray(raw)
		if err != nil {
			return errors.New(locale.T("作品保存位置无效"))
		}
		for _, dir := range dirs {
			if !filepath.IsAbs(dir) {
				return errors.New(locale.T("作品保存位置无效"))
			}
		}
	}
	if _, ok := obj["language"]; ok {
		language, err := stringField(obj, "language")
		if err != nil || !member(language, "", "system", "zh-Hans", "en") {
			return errors.New(locale.T("界面语言无效"))
		}
	}
	if value, ok := obj["theme"]; ok {
		var theme string
		if json.Unmarshal(value, &theme) != nil || !member(theme, "system", "light", "dark") {
			return errors.New(locale.T("主题无效"))
		}
	}
	if value, ok := obj["proxy"]; ok {
		proxy, err := object(value)
		if err != nil {
			return err
		}
		mode, err := stringField(proxy, "mode")
		if err != nil || !member(mode, "system", "direct", "manual") {
			return errors.New(locale.T("代理模式无效"))
		}
		for _, key := range []string{"url", "bypass"} {
			if _, err := stringField(proxy, key); err != nil {
				return err
			}
		}
	}
	if value, ok := obj["generationPreferences"]; ok {
		prefs, err := object(value)
		if err != nil {
			return err
		}
		if raw, ok := prefs["outputFormat"]; ok {
			var format string
			if string(raw) == "null" || json.Unmarshal(raw, &format) != nil || !member(format, "", "png", "jpeg", "webp", "gif", "svg") {
				return errors.New(locale.T("输出格式无效"))
			}
		}
		for key, bounds := range map[string][2]int{"gifFrameCount": {2, 64}, "gifFrameDurationMs": {20, 2000}} {
			if raw, ok := prefs[key]; ok {
				var value int
				if string(raw) == "null" || json.Unmarshal(raw, &value) != nil || (value != 0 && (value < bounds[0] || value > bounds[1])) {
					return errors.New(locale.T("GIF 动画偏好无效"))
				}
			}
		}
		if raw, ok := prefs["gifSize"]; ok {
			var size string
			if string(raw) == "null" || json.Unmarshal(raw, &size) != nil || !member(size, "", "512x512", "768x768", "1024x1024", "512x768", "768x512", "768x1024", "1024x768", "576x1024", "1024x576") {
				return errors.New(locale.T("GIF 单帧尺寸无效"))
			}
		}
		for _, key := range []string{"providerId", "model"} {
			value, err := stringField(prefs, key)
			if err != nil || len([]rune(value)) > 200 {
				return errors.New(locale.T("生成偏好无效"))
			}
		}
		ratio, err := stringField(prefs, "ratio")
		if err != nil || !member(ratio, "1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9") {
			return errors.New(locale.T("画面比例无效"))
		}
		resolution, err := stringField(prefs, "resolution")
		if err != nil || !member(resolution, "2k", "4k") {
			return errors.New(locale.T("分辨率无效"))
		}
	}
	if value, ok := obj["providers"]; ok {
		var providers []json.RawMessage
		if json.Unmarshal(value, &providers) != nil || len(providers) == 0 || len(providers) > 50 {
			return errors.New(locale.T("需要 1 到 50 个提供商"))
		}
		ids := map[string]bool{}
		for _, raw := range providers {
			provider, err := object(raw)
			if err != nil {
				return err
			}
			for _, key := range []string{"id", "name", "baseUrl", "apiKey", "generationsPath", "editsPath"} {
				if _, err := stringField(provider, key); err != nil {
					return err
				}
			}
			if raw, ok := provider["disabled"]; ok {
				var disabled bool
				if string(raw) == "null" || json.Unmarshal(raw, &disabled) != nil {
					return errors.New(locale.T("提供商启用状态无效"))
				}
			}
			id, _ := stringField(provider, "id")
			if strings.TrimSpace(id) == "" || ids[id] {
				return errors.New(locale.T("提供商 ID 无效或重复"))
			}
			ids[id] = true
			if _, err := stringArray(provider["models"]); err != nil {
				return err
			}
			resolutions := []string{"2k", "4k"}
			if raw, ok := provider["resolutions"]; ok {
				res, err := stringArray(raw)
				if err != nil || len(res) == 0 {
					return errors.New(locale.T("支持分辨率无效"))
				}
				seen := map[string]bool{}
				for _, v := range res {
					if !member(v, "2k", "4k") || seen[v] {
						return errors.New(locale.T("支持分辨率无效"))
					}
					seen[v] = true
				}
				resolutions = res
			}
			if _, ok := provider["defaultResolution"]; ok {
				resolution, err := stringField(provider, "defaultResolution")
				if err != nil || !member(resolution, resolutions...) {
					return errors.New(locale.T("提供商默认分辨率无效"))
				}
			}
		}
	}
	return nil
}
func validateHistory(raw []byte) error {
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return errors.New(locale.T("需要历史记录数组"))
	}
	ids := map[string]bool{}
	for _, raw := range rows {
		row, err := object(raw)
		if err != nil {
			return err
		}
		for _, key := range []string{"id", "prompt", "providerId", "providerName", "model", "size", "imagePath", "mimeType"} {
			if _, err := stringField(row, key); err != nil {
				return err
			}
		}
		id, _ := stringField(row, "id")
		if id == "" || ids[id] {
			return errors.New(locale.T("历史 ID 无效或重复"))
		}
		ids[id] = true
		for _, key := range []string{"createdAt", "width", "height", "referenceCount"} {
			var value int64
			raw, ok := row[key]
			if !ok || string(raw) == "null" || json.Unmarshal(raw, &value) != nil || value < 0 {
				return fmt.Errorf(locale.T("历史数字字段无效：%s"), key)
			}
		}
		for key, bounds := range map[string][2]int{"gifFrameCount": {2, 64}, "gifFrameDurationMs": {20, 2000}} {
			if raw, ok := row[key]; ok {
				var value int
				if string(raw) == "null" || json.Unmarshal(raw, &value) != nil || (value != 0 && (value < bounds[0] || value > bounds[1])) {
					return errors.New(locale.T("GIF 历史参数无效"))
				}
			}
		}
		var favorite bool
		value, ok := row["favorite"]
		if !ok || string(value) == "null" || json.Unmarshal(value, &favorite) != nil {
			return errors.New(locale.T("收藏字段无效"))
		}
		if _, err := stringArray(row["referencePaths"]); err != nil {
			return err
		}
	}
	return nil
}
