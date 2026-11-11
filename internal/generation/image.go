package generation

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"image2ai/internal/locale"
	"io"
	"net/http"
	"strings"
	"unicode"

	_ "golang.org/x/image/webp"
)

const MaxImageBytes = 40 * 1024 * 1024
const MaxReferenceBytes = 20 * 1024 * 1024

// ValidateImageConfig bounds encoded image bytes and decoded dimensions without
// allocating the pixel buffer. Callers must still handle full decoding errors.
func ValidateImageConfig(data []byte) error {
	_, _, _, err := imageConfig(data)
	return err
}

// ImageSize returns the pixel size of bounded, encoded image bytes.
func ImageSize(data []byte) (image.Point, error) {
	_, w, h, err := imageConfig(data)
	return image.Pt(w, h), err
}

func imageConfig(data []byte) (mime string, width, height int, err error) {
	if len(data) == 0 || len(data) > MaxImageBytes {
		return "", 0, 0, errors.New(locale.T("图片为空或超过 40 MB。"))
	}
	if isSVG(data) {
		w, h, err := svgSize(data)
		return "image/svg+xml", w, h, err
	}
	if bytes.HasPrefix(data, []byte("GIF8")) {
		if err := validateGIF(data); err != nil {
			return "", 0, 0, err
		}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, errors.New(locale.T("图片无法解码，请使用 PNG、JPEG、WebP、GIF 或 SVG。"))
	}
	mime = map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp", "gif": "image/gif"}[format]
	if mime == "" || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 67108864 {
		return "", 0, 0, errors.New(locale.T("图片格式或像素尺寸无效（最多 6400 万像素）。"))
	}
	return mime, config.Width, config.Height, nil
}

func decodeImage(data []byte) (mime string, width, height int, err error) {
	mime, width, height, err = imageConfig(data)
	if err != nil {
		return "", 0, 0, err
	}
	if mime == "image/svg+xml" {
		_, err = renderSVG(data, 64, 64)
		return mime, width, height, err
	}
	if mime == "image/gif" {
		_, err = gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			err = errors.New(locale.T("GIF 数据不完整或无法解码。"))
		}
		return mime, width, height, err
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return "", 0, 0, errors.New(locale.T("图片数据不完整或无法解码。"))
	}
	return mime, width, height, nil
}
func decodeBase64(value string, limit int) ([]byte, error) {
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	if len(value) == 0 || len(value) > base64.StdEncoding.EncodedLen(limit) {
		return nil, errors.New(locale.T("图片 Base64 数据无效或过大。"))
	}
	data, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		data, err = base64.RawStdEncoding.Strict().DecodeString(value)
	}
	if err != nil || len(data) > limit {
		return nil, errors.New(locale.T("图片 Base64 数据无效或过大。"))
	}
	return data, nil
}
func dataURL(value string, limit int) ([]byte, error) {
	header, payload, ok := strings.Cut(value, ",")
	if !ok {
		return nil, errors.New(locale.T("参考图必须是 PNG、JPEG 或 WebP 图片。"))
	}
	switch strings.ToLower(header) {
	case "data:image/png;base64", "data:image/jpeg;base64", "data:image/jpg;base64", "data:image/webp;base64":
	default:
		return nil, errors.New(locale.T("参考图必须是 PNG、JPEG 或 WebP 图片。"))
	}
	return decodeBase64(payload, limit)
}
func ValidateReference(value string) ([]byte, string, error) {
	data, err := dataURL(value, MaxReferenceBytes)
	if err != nil {
		return nil, "", err
	}
	mime, _, _, err := decodeImage(data)
	return data, mime, err
}

// ImageFormat identifies validated output bytes without trusting the extension.
func ImageFormat(data []byte) (string, error) {
	mime, _, _, err := imageConfig(data)
	return imageExtension(mime), err
}

func ValidOutputFormat(value string) bool {
	switch value {
	case "", "png", "jpeg", "webp", "gif", "svg":
		return true
	}
	return false
}
func readLimited(response *http.Response, limit int64) ([]byte, error) {
	defer response.Body.Close()
	if response.ContentLength > limit {
		return nil, errors.New(locale.T("接口响应过大。"))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, errors.New(locale.T("读取接口响应失败。"))
	}
	if int64(len(data)) > limit {
		return nil, errors.New(locale.T("接口响应过大。"))
	}
	return data, nil
}
func imageExtension(mime string) string {
	switch mime {
	case "image/gif":
		return "gif"
	case "image/svg+xml":
		return "svg"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	default:
		return "png"
	}
}
