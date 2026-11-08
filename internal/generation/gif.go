package generation

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"time"
)

const maxGIFFrames = 256
const maxGIFPixels = 16 * 1024 * 1024

// Check all frame descriptors before DecodeAll can allocate frame buffers.
func validateGIF(data []byte) error {
	fail := errors.New("GIF 无效或过大（最多 256 帧、总计 1600 万帧像素）。")
	if len(data) < 13 || (string(data[:6]) != "GIF87a" && string(data[:6]) != "GIF89a") {
		return fail
	}
	w, h := int(binary.LittleEndian.Uint16(data[6:8])), int(binary.LittleEndian.Uint16(data[8:10]))
	if w <= 0 || h <= 0 || int64(w)*int64(h) > maxGIFPixels {
		return fail
	}
	pos, frames := 13, 0
	if data[10]&0x80 != 0 {
		pos += 3 * (1 << ((data[10] & 7) + 1))
	}
	blocks := func() bool {
		for pos < len(data) {
			n := int(data[pos])
			pos++
			if n == 0 {
				return true
			}
			if pos+n > len(data) {
				return false
			}
			pos += n
		}
		return false
	}
	for pos < len(data) {
		marker := data[pos]
		pos++
		switch marker {
		case 0x3b:
			if frames > 0 && pos == len(data) {
				return nil
			}
			return fail
		case 0x21:
			if pos >= len(data) {
				return fail
			}
			pos++
			if !blocks() {
				return fail
			}
		case 0x2c:
			if pos+9 > len(data) {
				return fail
			}
			x, y := int(binary.LittleEndian.Uint16(data[pos:])), int(binary.LittleEndian.Uint16(data[pos+2:]))
			fw, fh := int(binary.LittleEndian.Uint16(data[pos+4:])), int(binary.LittleEndian.Uint16(data[pos+6:]))
			packed := data[pos+8]
			pos += 9
			frames++
			if frames > maxGIFFrames || int64(frames)*int64(w)*int64(h) > maxGIFPixels || fw <= 0 || fh <= 0 || x+fw > w || y+fh > h {
				return fail
			}
			if packed&0x80 != 0 {
				pos += 3 * (1 << ((packed & 7) + 1))
			}
			if pos >= len(data) {
				return fail
			}
			pos++
			if !blocks() {
				return fail
			}
		default:
			return fail
		}
	}
	return fail
}

type Animation struct {
	Frames    []image.Image
	Delays    []time.Duration
	LoopCount int
}

func gifBackground(decoded *gif.GIF) color.Color {
	background := color.Color(color.Transparent)
	if palette, ok := decoded.Config.ColorModel.(color.Palette); ok && int(decoded.BackgroundIndex) < len(palette) {
		background = palette[decoded.BackgroundIndex]
	}
	for _, p := range decoded.Image[0].Palette {
		if _, _, _, alpha := p.RGBA(); alpha == 0 {
			return color.Transparent
		}
	}
	return background
}

// DecodeAnimation composites partial frames and applies GIF disposal rules.
func DecodeAnimation(data []byte) (*Animation, error) {
	if len(data) > MaxImageBytes {
		return nil, errors.New("GIF 文件超过 40 MB。")
	}
	if err := validateGIF(data); err != nil {
		return nil, err
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("GIF 数据不完整或无法解码。")
	}
	bounds := image.Rect(0, 0, decoded.Config.Width, decoded.Config.Height)
	canvas := image.NewRGBA(bounds)
	background := gifBackground(decoded)
	draw.Draw(canvas, bounds, image.NewUniform(background), image.Point{}, draw.Src)
	result := &Animation{LoopCount: decoded.LoopCount}
	for i, frame := range decoded.Image {
		var previous *image.RGBA
		disposal := byte(0)
		if i < len(decoded.Disposal) {
			disposal = decoded.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			previous = image.NewRGBA(bounds)
			copy(previous.Pix, canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		output := image.NewRGBA(bounds)
		copy(output.Pix, canvas.Pix)
		result.Frames = append(result.Frames, output)
		delay := time.Duration(decoded.Delay[i]) * 10 * time.Millisecond
		if delay <= 10*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		result.Delays = append(result.Delays, delay)
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			canvas = previous
		}
	}
	return result, nil
}
