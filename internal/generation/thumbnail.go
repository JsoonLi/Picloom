package generation

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	stddraw "image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"image2ai/internal/locale"

	"golang.org/x/image/draw"
)

// MaxThumbnailSide bounds requested thumbnail sizes.
const MaxThumbnailSide = 2048

// Thumbnail decodes a bounded image and shrinks it to fit maxW×maxH at its
// own proportions. Images already small enough are returned as decoded.
func Thumbnail(data []byte, maxW, maxH int) (image.Image, error) {
	if maxW < 1 || maxH < 1 || maxW > MaxThumbnailSide || maxH > MaxThumbnailSide {
		return nil, errors.New(locale.T("缩略图尺寸无效。"))
	}
	mime, w, h, err := imageConfig(data)
	if err != nil {
		return nil, err
	}
	if isSVG(data) {
		return renderSVG(data, maxW, maxH)
	}
	var source image.Image
	if mime == "image/gif" {
		var decoded *gif.GIF
		decoded, err = gif.DecodeAll(bytes.NewReader(data))
		if err == nil {
			canvas := image.NewRGBA(image.Rect(0, 0, w, h))
			stddraw.Draw(canvas, canvas.Bounds(), image.NewUniform(gifBackground(decoded)), image.Point{}, stddraw.Src)
			first := decoded.Image[0]
			stddraw.Draw(canvas, first.Bounds(), first, first.Bounds().Min, stddraw.Over)
			source = canvas
		}
	} else {
		source, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, errors.New(locale.T("图片数据不完整或无法解码。"))
	}
	bounds := source.Bounds()
	w, h = bounds.Dx(), bounds.Dy()
	if w <= maxW && h <= maxH {
		return source, nil
	}
	scale := min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	return downscale(source, max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))), nil
}

// EncodeThumbnail stores opaque thumbnails as JPEG, which is small and fast
// to decode, and keeps PNG for any with transparency.
func EncodeThumbnail(img image.Image) ([]byte, error) {
	var out bytes.Buffer
	var err error
	if opaque(img) {
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 88})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, img)
	}
	return out.Bytes(), err
}

func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}

// downscale averages whole blocks of source pixels first, so every pixel
// contributes, and leaves only a final shrink of less than two to bilinear
// filtering, which on its own would sample a few pixels and alias.
func downscale(source image.Image, tw, th int) *image.RGBA {
	bounds := source.Bounds()
	if k := min(bounds.Dx()/tw, bounds.Dy()/th); k >= 2 {
		source = boxReduce(source, k)
		bounds = source.Bounds()
	}
	if rgba, ok := source.(*image.RGBA); ok && bounds.Dx() == tw && bounds.Dy() == th {
		return rgba
	}
	target := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.BiLinear.Scale(target, target.Bounds(), source, bounds, draw.Src, nil)
	return target
}

// boxReduce returns source k times smaller, each pixel the premultiplied
// average of the k×k block it covers; partial edge blocks average the
// pixels they have.
func boxReduce(source image.Image, k int) *image.RGBA {
	bounds := source.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	w, h := (sw+k-1)/k, (sh+k-1)/k
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	sums := make([]uint64, 4*w)
	row := make([]uint8, 4*sw)
	for oy := range h {
		clear(sums)
		y0, y1 := oy*k, min((oy+1)*k, sh)
		for y := y0; y < y1; y++ {
			readRow(source, bounds.Min.Y+y, row)
			for ox := range w {
				x0, x1 := ox*k, min((ox+1)*k, sw)
				s, p := sums[4*ox:4*ox+4], row[4*x0:4*x1]
				for i := 0; i < len(p); i += 4 {
					s[0] += uint64(p[i])
					s[1] += uint64(p[i+1])
					s[2] += uint64(p[i+2])
					s[3] += uint64(p[i+3])
				}
			}
		}
		o := out.Pix[oy*out.Stride:]
		for ox := range w {
			n := uint64((min((ox+1)*k, sw) - ox*k) * (y1 - y0))
			for c := range 4 {
				o[4*ox+c] = uint8((sums[4*ox+c] + n/2) / n)
			}
		}
	}
	return out
}

// readRow fills row with the premultiplied RGBA pixels of line y, using
// direct pixel access for the formats the standard decoders produce.
func readRow(source image.Image, y int, row []uint8) {
	bounds := source.Bounds()
	switch s := source.(type) {
	case *image.RGBA:
		copy(row, s.Pix[s.PixOffset(bounds.Min.X, y):])
	case *image.NRGBA:
		p := s.Pix[s.PixOffset(bounds.Min.X, y):]
		for i := 0; i < len(row); i += 4 {
			a := uint32(p[i+3])
			row[i] = uint8((uint32(p[i])*a + 127) / 255)
			row[i+1] = uint8((uint32(p[i+1])*a + 127) / 255)
			row[i+2] = uint8((uint32(p[i+2])*a + 127) / 255)
			row[i+3] = uint8(a)
		}
	case *image.YCbCr:
		for x, i := bounds.Min.X, 0; i < len(row); x, i = x+1, i+4 {
			ci := s.COffset(x, y)
			row[i], row[i+1], row[i+2] = color.YCbCrToRGB(s.Y[s.YOffset(x, y)], s.Cb[ci], s.Cr[ci])
			row[i+3] = 0xff
		}
	case *image.Gray:
		p := s.Pix[s.PixOffset(bounds.Min.X, y):]
		for x, i := 0, 0; i < len(row); x, i = x+1, i+4 {
			row[i], row[i+1], row[i+2], row[i+3] = p[x], p[x], p[x], 0xff
		}
	default:
		for x, i := bounds.Min.X, 0; i < len(row); x, i = x+1, i+4 {
			r, g, b, a := source.At(x, y).RGBA()
			row[i], row[i+1], row[i+2], row[i+3] = uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8)
		}
	}
}
