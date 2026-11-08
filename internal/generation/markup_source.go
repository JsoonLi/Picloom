package generation

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
)

// PrepareMarkupSource makes JPEG orientation explicit in its pixels. This
// keeps the provider's source and overlay aligned with the upright preview,
// including mirrored photographs. Other sources retain their original bytes.
func PrepareMarkupSource(data []byte) ([]byte, string, image.Point, error) {
	mime, w, h, err := imageConfig(data)
	if err != nil {
		return nil, "", image.Point{}, err
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
		return nil, "", image.Point{}, errors.New("标注编辑支持 PNG、JPEG 或 WebP 原图，请先转换图片格式。")
	}
	if !validMarkupSize(image.Pt(w, h)) {
		return nil, "", image.Point{}, errors.New("原图尺寸过大，无法添加标注。")
	}
	if mime == "image/jpeg" {
		if orientation := jpegOrientation(data); orientation > 1 {
			source, _, e := image.Decode(bytes.NewReader(data))
			if e != nil {
				return nil, "", image.Point{}, errors.New("原图无法解码。")
			}
			w, h = source.Bounds().Dx(), source.Bounds().Dy()
			if orientation >= 5 {
				w, h = h, w
			}
			out := image.NewNRGBA(image.Rect(0, 0, w, h))
			sw, sh := source.Bounds().Dx(), source.Bounds().Dy()
			for y := range h {
				for x := range w {
					sx, sy := x, y
					switch orientation {
					case 2:
						sx = sw - 1 - x
					case 3:
						sx, sy = sw-1-x, sh-1-y
					case 4:
						sy = sh - 1 - y
					case 5:
						sx, sy = y, x
					case 6:
						sx, sy = y, sh-1-x
					case 7:
						sx, sy = sw-1-y, sh-1-x
					case 8:
						sx, sy = sw-1-y, x
					}
					out.Set(x, y, source.At(sx+source.Bounds().Min.X, sy+source.Bounds().Min.Y))
				}
			}
			var encoded bytes.Buffer
			if e = png.Encode(&encoded, out); e != nil {
				return nil, "", image.Point{}, e
			}
			data, mime = encoded.Bytes(), "image/png"
		}
	}
	if len(data) > MaxReferenceBytes {
		return nil, "", image.Point{}, errors.New("原图校正后超过 20 MB，请缩小图片再添加标注。")
	}
	return data, mime, image.Pt(w, h), nil
}

func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			break
		}
		marker := data[pos+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[pos+2:]))
		if length < 2 || length > len(data)-pos-2 {
			break
		}
		segment := data[pos+4 : pos+2+length]
		if marker == 0xe1 && len(segment) >= 14 && bytes.Equal(segment[:6], []byte("Exif\x00\x00")) {
			tiff := segment[6:]
			var order binary.ByteOrder
			switch string(tiff[:2]) {
			case "II":
				order = binary.LittleEndian
			case "MM":
				order = binary.BigEndian
			default:
				return 1
			}
			if order.Uint16(tiff[2:]) != 42 {
				return 1
			}
			offset := uint64(order.Uint32(tiff[4:]))
			if offset < 8 || offset+2 > uint64(len(tiff)) {
				return 1
			}
			count := int(order.Uint16(tiff[offset:]))
			for i, entry := 0, int(offset)+2; i < count && entry+12 <= len(tiff); i, entry = i+1, entry+12 {
				if order.Uint16(tiff[entry:]) == 0x112 && order.Uint16(tiff[entry+2:]) == 3 && order.Uint32(tiff[entry+4:]) == 1 {
					value := int(order.Uint16(tiff[entry+8:]))
					if value >= 1 && value <= 8 {
						return value
					}
				}
			}
		}
		pos += length + 2
	}
	return 1
}
