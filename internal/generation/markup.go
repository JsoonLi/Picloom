package generation

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"

	"golang.org/x/image/vector"
)

const MaxMarkupPoints = 20000
const MaxMarkupStrokes = 256

// MarkupStroke coordinates and widths are in source-image pixels, independent
// of window size, display scaling and the viewer's zoom or pan.
type MarkupStroke struct {
	Tool   string
	Points [][2]float32
	Width  float32
}

var MarkupColor = color.NRGBA{R: 234, G: 66, B: 58, A: 255}

// MarkupPolyline is shared by native preview and PNG export.
func MarkupPolyline(s MarkupStroke) [][2]float32 {
	if len(s.Points) < 2 || s.Tool == "pen" {
		return s.Points
	}
	a, b := s.Points[0], s.Points[len(s.Points)-1]
	if s.Tool == "rectangle" {
		return [][2]float32{a, {b[0], a[1]}, b, {a[0], b[1]}, a}
	}
	cx, cy := (a[0]+b[0])/2, (a[1]+b[1])/2
	rx, ry := float64(b[0]-a[0])/2, float64(b[1]-a[1])/2
	points := make([][2]float32, 97)
	for i := range points {
		angle := 2 * math.Pi * float64(i) / float64(len(points)-1)
		points[i] = [2]float32{cx + float32(rx*math.Cos(angle)), cy + float32(ry*math.Sin(angle))}
	}
	points[len(points)-1] = points[0]
	return points
}

func validMarkupSize(size image.Point) bool {
	return size.X > 0 && size.Y > 0 && size.X <= 32768 && size.Y <= 32768 && int64(size.X)*int64(size.Y) <= 67108864
}

// RenderMarkupPNG never composites the source into the transparent overlay.
// Rasterization/encoding must be called by a worker, never during a UI frame.
func RenderMarkupPNG(size image.Point, strokes []MarkupStroke) ([]byte, error) {
	if !validMarkupSize(size) || len(strokes) == 0 || len(strokes) > MaxMarkupStrokes {
		return nil, errors.New("请先在图片上添加标注（最多 256 笔）。")
	}
	total := 0
	for _, s := range strokes {
		total += len(s.Points)
		if len(s.Points) == 0 || total > MaxMarkupPoints || (s.Tool != "pen" && s.Tool != "rectangle" && s.Tool != "ellipse") || !finite(s.Width) || s.Width <= 0 || s.Width > float32(max(size.X, size.Y)) {
			return nil, errors.New("标注数据无效或过多，请撤销部分标注后重试。")
		}
		for _, p := range s.Points {
			if !finite(p[0]) || !finite(p[1]) || p[0] < 0 || p[1] < 0 || p[0] > float32(size.X) || p[1] > float32(size.Y) {
				return nil, errors.New("标注坐标超出原图范围。")
			}
		}
	}
	z := vector.NewRasterizer(size.X, size.Y)
	for _, s := range strokes {
		points := MarkupPolyline(s)
		r := s.Width / 2
		for i, p := range points {
			markupDisc(z, p[0], p[1], r)
			if i == 0 {
				continue
			}
			a := points[i-1]
			dx, dy := p[0]-a[0], p[1]-a[1]
			length := float32(math.Hypot(float64(dx), float64(dy)))
			if length == 0 {
				continue
			}
			nx, ny := -dy*r/length, dx*r/length
			z.MoveTo(a[0]+nx, a[1]+ny)
			z.LineTo(a[0]-nx, a[1]-ny)
			z.LineTo(p[0]-nx, p[1]-ny)
			z.LineTo(p[0]+nx, p[1]+ny)
			z.ClosePath()
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
	z.Draw(out, out.Bounds(), image.NewUniform(MarkupColor), image.Point{})
	var data bytes.Buffer
	if err := png.Encode(&data, out); err != nil {
		return nil, errors.New("无法生成标注层，请重试。")
	}
	if data.Len() > MaxReferenceBytes {
		return nil, errors.New("标注层超过 20 MB，请减少标注后重试。")
	}
	return data.Bytes(), nil
}

func finite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }

func markupDisc(z *vector.Rasterizer, x, y, r float32) {
	const k = 0.55228475
	d := r * k
	z.MoveTo(x+r, y)
	z.CubeTo(x+r, y+d, x+d, y+r, x, y+r)
	z.CubeTo(x-d, y+r, x-r, y+d, x-r, y)
	z.CubeTo(x-r, y-d, x-d, y-r, x, y-r)
	z.CubeTo(x+d, y-r, x+r, y-d, x+r, y)
	z.ClosePath()
}

func validateMarkup(value string, source []byte) ([]byte, error) {
	header, _, _ := strings.Cut(value, ",")
	if !strings.EqualFold(header, "data:image/png;base64") {
		return nil, errors.New("标注层必须是透明 PNG 图片。")
	}
	data, err := dataURL(value, MaxReferenceBytes)
	if err != nil {
		return nil, errors.New("标注层数据无效或超过 20 MB。")
	}
	size, err := ImageSize(data)
	if err != nil || !validMarkupSize(size) {
		return nil, errors.New("标注层尺寸无效。")
	}
	original, err := ImageSize(source)
	if err != nil || original != size {
		return nil, errors.New("标注层尺寸必须与原图一致。")
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("标注层不是完整的 PNG 图片。")
	}
	transparent, marked := false, false
	for y := 0; y < size.Y && !(transparent && marked); y++ {
		for x := 0; x < size.X && !(transparent && marked); x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			transparent = transparent || alpha == 0
			marked = marked || alpha > 0
		}
	}
	if !transparent || !marked {
		return nil, errors.New("请使用背景透明且包含可见标注的 PNG 标注层。")
	}
	return data, nil
}
