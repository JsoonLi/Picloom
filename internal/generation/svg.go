package generation

import (
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image2ai/internal/locale"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func isSVG(data []byte) bool {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	return bytes.HasPrefix(data, []byte("<svg")) || bytes.HasPrefix(data, []byte("<?xml")) || bytes.HasPrefix(data, []byte("<!--"))
}

// SVGs are self-contained documents, never a source of file or network reads.
// Limit geometry complexity before sending it to the bounded rasterizer.
func svgSize(data []byte) (int, int, error) {
	fail := errors.New(locale.T("SVG 无效或包含不支持的外部资源、脚本、滤镜或过于复杂的图形。"))
	if len(data) == 0 || len(data) > 4<<20 {
		return 0, 0, fail
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, nodes, uses := 0, 0, 0
	var ids []string
	useTargets, containsUse := map[string]bool{}, map[string]bool{}
	var root *xml.StartElement
	allowed := " svg g defs symbol use path rect circle ellipse line polyline polygon text tspan title desc style linearGradient radialGradient stop clipPath mask "
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, 0, fail
		}
		switch tok := token.(type) {
		case xml.StartElement:
			id := ""
			for _, attr := range tok.Attr {
				if attr.Name.Local == "id" {
					id = attr.Value
				}
			}
			ids = append(ids, id)
			depth++
			nodes++
			if depth > 48 || nodes > 8000 || (tok.Name.Space != "" && tok.Name.Space != "http://www.w3.org/2000/svg") || !strings.Contains(allowed, " "+tok.Name.Local+" ") {
				return 0, 0, fail
			}
			if root == nil {
				if tok.Name.Local != "svg" {
					return 0, 0, fail
				}
				copy := tok
				root = &copy
			} else if depth == 1 {
				return 0, 0, fail
			}
			if tok.Name.Local == "use" {
				for _, parent := range ids {
					if parent != "" {
						containsUse[parent] = true
					}
				}
				for _, attr := range tok.Attr {
					if attr.Name.Local == "href" {
						useTargets[strings.TrimPrefix(attr.Value, "#")] = true
					}
				}
				uses++
				if uses > 64 {
					return 0, 0, fail
				}
			}
			for _, attr := range tok.Attr {
				key, value := strings.ToLower(attr.Name.Local), strings.TrimSpace(attr.Value)
				if strings.HasPrefix(key, "on") || key == "base" || len(value) > 262144 {
					return 0, 0, fail
				}
				if key == "href" && !strings.HasPrefix(value, "#") {
					return 0, 0, fail
				}
				if !safeSVGStyle(value) {
					return 0, 0, fail
				}
			}
		case xml.EndElement:
			depth--
			ids = ids[:len(ids)-1]
		case xml.Directive:
			return 0, 0, fail
		case xml.ProcInst:
			if tok.Target != "xml" {
				return 0, 0, fail
			}
		case xml.CharData:
			if !safeSVGStyle(string(tok)) {
				return 0, 0, fail
			}
			if depth == 0 && strings.TrimSpace(string(tok)) != "" {
				return 0, 0, fail
			}
		}
	}
	if root == nil || depth != 0 {
		return 0, 0, fail
	}
	// Nested or circular use expansions can grow without bound even in a
	// tiny source document. Only references to non-expanding shapes are allowed.
	for target := range useTargets {
		if containsUse[target] {
			return 0, 0, fail
		}
	}
	var w, h float64
	var view []float64
	for _, attr := range root.Attr {
		switch attr.Name.Local {
		case "width":
			w = svgLength(attr.Value)
		case "height":
			h = svgLength(attr.Value)
		case "viewBox":
			values := strings.Fields(strings.ReplaceAll(attr.Value, ",", " "))
			if len(values) != 4 {
				return 0, 0, fail
			}
			for _, v := range values {
				n, e := strconv.ParseFloat(v, 64)
				if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return 0, 0, fail
				}
				view = append(view, n)
			}
		}
	}
	if len(view) == 4 {
		if view[2] <= 0 || view[3] <= 0 {
			return 0, 0, fail
		}
		if w <= 0 && h > 0 {
			w = h * view[2] / view[3]
		}
		if h <= 0 && w > 0 {
			h = w * view[3] / view[2]
		}
		if w <= 0 && h <= 0 {
			w, h = view[2], view[3]
		}
	}
	if math.IsNaN(w) || math.IsNaN(h) || w <= 0 || h <= 0 || w > 16384 || h > 16384 || w*h > 67108864 {
		return 0, 0, fail
	}
	return int(math.Ceil(w)), int(math.Ceil(h)), nil
}

func svgLength(value string) float64 {
	value = strings.TrimSpace(value)
	for _, unit := range []struct {
		suffix string
		scale  float64
	}{{"px", 1}, {"pt", 96.0 / 72}, {"pc", 16}, {"mm", 96.0 / 25.4}, {"cm", 96.0 / 2.54}, {"in", 96}} {
		if strings.HasSuffix(value, unit.suffix) {
			n, _ := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			return n * unit.scale
		}
	}
	n, _ := strconv.ParseFloat(value, 64)
	return n
}

func safeSVGStyle(value string) bool {
	value = strings.ToLower(value)
	if strings.ContainsAny(value, "\\") || strings.Contains(value, "@import") || strings.Contains(value, "/*") || strings.Contains(value, "expression(") {
		return false
	}
	for {
		_, rest, ok := strings.Cut(value, "url(")
		if !ok {
			return true
		}
		target, tail, ok := strings.Cut(rest, ")")
		if !ok || !strings.HasPrefix(strings.Trim(strings.TrimSpace(target), "\"'"), "#") {
			return false
		}
		value = tail
	}
}

var svgRenderSlot = make(chan struct{}, 1)

func renderSVG(data []byte, maxW, maxH int) (result image.Image, resultErr error) {
	defer func() {
		if recover() != nil {
			result = nil
			resultErr = errors.New(locale.T("SVG 无法渲染，请检查图形与字体。"))
		}
	}()
	w, h, err := svgSize(data)
	if err != nil {
		return nil, err
	}
	scale := min(1.0, float64(maxW)/float64(w), float64(maxH)/float64(h))
	tw, th := max(1, int(math.Round(float64(w)*scale))), max(1, int(math.Round(float64(h)*scale)))
	svgRenderSlot <- struct{}{}
	defer func() { <-svgRenderSlot }()
	data, err = outlineSVGText(data)
	if err != nil {
		return nil, err
	}
	document, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil || document.ViewBox.W <= 0 || document.ViewBox.H <= 0 {
		return nil, errors.New(locale.T("SVG 无法渲染，请检查图形内容。"))
	}
	output := image.NewRGBA(image.Rect(0, 0, tw, th))
	scanner := rasterx.NewScannerGV(tw, th, output, output.Bounds())
	dasher := rasterx.NewDasher(tw, th, scanner)
	document.SetTarget(0, 0, float64(tw), float64(th))
	document.Draw(dasher, 1)
	return output, nil
}
