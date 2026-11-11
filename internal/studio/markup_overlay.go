package studio

import "errors"
import "image2ai/internal/locale"
import (
	"bytes"
	"image"
	"image/draw"
	"image/png"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/generation"
)

func readMarkupPNG(value string, size image.Point) (image.Image, error) {
	data, mime, err := generation.ValidateReference(value)
	if err != nil {
		return nil, err
	}
	actual, err := generation.ImageSize(data)
	if err != nil || actual != size || mime != "image/png" {
		return nil, errors.New(locale.T("原标注层必须为同尺寸的 PNG 图片。"))
	}
	return png.Decode(bytes.NewReader(data))
}

func decodeMarkupBase(value, source string) (*ui.Bitmap, error) {
	data, _, err := generation.ValidateReference(source)
	if err != nil {
		return nil, err
	}
	size, err := generation.ImageSize(data)
	if err != nil {
		return nil, err
	}
	img, err := readMarkupPNG(value, size)
	if err != nil {
		return nil, err
	}
	return ui.NewBitmap(img), nil
}

// Restored annotations are a transparent base layer; new strokes are painted
// over it, never over the source image. Called on the preparation worker.
func renderMarkupOverlay(size image.Point, strokes []generation.MarkupStroke, base string) (string, error) {
	if len(strokes) == 0 {
		return base, nil
	}
	data, err := generation.RenderMarkupPNG(size, strokes)
	if err != nil {
		return "", err
	}
	if base == "" {
		return generation.DataURL(data, "image/png"), nil
	}
	old, err := readMarkupPNG(base, size)
	if err != nil {
		return "", err
	}
	newMarks, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	out := image.NewNRGBA(image.Rectangle{Max: size})
	draw.Draw(out, out.Bounds(), old, old.Bounds().Min, draw.Src)
	draw.Draw(out, out.Bounds(), newMarks, newMarks.Bounds().Min, draw.Over)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, out); err != nil {
		return "", err
	}
	if encoded.Len() > generation.MaxReferenceBytes {
		return "", errors.New(locale.T("标注层超过 20 MB，请减少标注后重试。"))
	}
	return generation.DataURL(encoded.Bytes(), "image/png"), nil
}
