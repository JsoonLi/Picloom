package generation

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var svgFonts struct {
	sync.Once
	fonts []*sfnt.Font
}

func svgTextFonts() []*sfnt.Font {
	svgFonts.Do(func() {
		files := []string{"/System/Library/Fonts/PingFang.ttc", "/System/Library/Fonts/Supplemental/Arial.ttf"}
		if runtime.GOOS == "windows" {
			files = []string{"C:/Windows/Fonts/msyh.ttc", "C:/Windows/Fonts/arial.ttf"}
		}
		for _, path := range files {
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 32<<20 {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if f, err := sfnt.Parse(data); err == nil {
				svgFonts.fonts = append(svgFonts.fonts, f)
			} else if collection, err := sfnt.ParseCollection(data); err == nil {
				if f, err := collection.Font(0); err == nil {
					svgFonts.fonts = append(svgFonts.fonts, f)
				}
			}
		}
		if fallback, err := sfnt.Parse(goregular.TTF); err == nil {
			svgFonts.fonts = append(svgFonts.fonts, fallback)
		}
	})
	return svgFonts.fonts
}

type svgTextState struct {
	x, y, size float64
	anchor     string
	text       bool
	tag        string
}

// The native SVG rasterizer draws geometry. Outline bounded text with platform
// fonts for its preview only; the saved and exported SVG is never rewritten.
func outlineSVGText(data []byte) ([]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	states := []svgTextState{{size: 16}}
	glyphs := 0
	fail := errors.New("SVG 文字无法绘制或过于复杂。")
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fail
		}
		switch tok := token.(type) {
		case xml.StartElement:
			state := states[len(states)-1]
			state.tag = tok.Name.Local
			if tok.Name.Local == "text" || tok.Name.Local == "tspan" {
				state.text = true
			}
			for _, attr := range tok.Attr {
				switch attr.Name.Local {
				case "x":
					if state.text {
						state.x = svgLength(attr.Value)
					}
				case "y":
					if state.text {
						state.y = svgLength(attr.Value)
					}
				case "dx":
					if state.text {
						state.x += svgLength(attr.Value)
					}
				case "dy":
					if state.text {
						state.y += svgLength(attr.Value)
					}
				case "font-size":
					state.size = svgLength(attr.Value)
				case "text-anchor":
					state.anchor = attr.Value
				case "style":
					for _, prop := range strings.Split(attr.Value, ";") {
						key, val, ok := strings.Cut(prop, ":")
						if ok {
							switch strings.TrimSpace(key) {
							case "font-size":
								state.size = svgLength(strings.TrimSpace(val))
							case "text-anchor":
								state.anchor = strings.TrimSpace(val)
							}
						}
					}
				}
			}
			if state.size <= 0 || state.size > 1024 {
				return nil, fail
			}
			states = append(states, state)
			if tok.Name.Local == "text" || tok.Name.Local == "tspan" {
				tok.Name.Local = "g"
			}
			if err = encoder.EncodeToken(tok); err != nil {
				return nil, fail
			}
		case xml.EndElement:
			state := states[len(states)-1]
			states = states[:len(states)-1]
			if state.tag == "text" || state.tag == "tspan" {
				tok.Name.Local = "g"
				if states[len(states)-1].text {
					states[len(states)-1].x, states[len(states)-1].y = state.x, state.y
				}
			}
			if err = encoder.EncodeToken(tok); err != nil {
				return nil, fail
			}
		case xml.CharData:
			state := &states[len(states)-1]
			if !state.text || strings.TrimSpace(string(tok)) == "" {
				if err = encoder.EncodeToken(tok); err != nil {
					return nil, fail
				}
				continue
			}
			text := strings.Join(strings.Fields(string(tok)), " ")
			glyphs += len([]rune(text))
			if glyphs > 4096 {
				return nil, fail
			}
			path, advance, err := svgGlyphPath(text, state.size)
			if err != nil {
				return nil, fail
			}
			x := state.x
			if state.anchor == "middle" {
				x -= advance / 2
			} else if state.anchor == "end" {
				x -= advance
			}
			start := xml.StartElement{Name: xml.Name{Local: "path"}, Attr: []xml.Attr{{Name: xml.Name{Local: "d"}, Value: path}, {Name: xml.Name{Local: "transform"}, Value: fmt.Sprintf("translate(%g %g)", x, state.y)}}}
			if err = encoder.EncodeToken(start); err != nil {
				return nil, fail
			}
			if err = encoder.EncodeToken(start.End()); err != nil {
				return nil, fail
			}
			state.x += advance
		default:
			if err = encoder.EncodeToken(token); err != nil {
				return nil, fail
			}
		}
		if err = encoder.Flush(); err != nil || out.Len() > 8<<20 {
			return nil, fail
		}
	}
	return out.Bytes(), nil
}

func svgGlyphPath(text string, size float64) (string, float64, error) {
	var path strings.Builder
	var buffer sfnt.Buffer
	x := 0.0
	ppem := fixed.Int26_6(size * 64)
	for _, r := range text {
		var face *sfnt.Font
		var glyph sfnt.GlyphIndex
		for _, candidate := range svgTextFonts() {
			index, err := candidate.GlyphIndex(&buffer, r)
			if err == nil && index != 0 {
				face, glyph = candidate, index
				break
			}
		}
		if face == nil {
			face = svgTextFonts()[len(svgTextFonts())-1]
		}
		segments, err := face.LoadGlyph(&buffer, glyph, ppem, nil)
		if err != nil {
			return "", 0, err
		}
		if len(segments) > 4096 {
			return "", 0, errors.New("glyph too complex")
		}
		for _, segment := range segments {
			count := 1
			command := "M"
			switch segment.Op {
			case sfnt.SegmentOpLineTo:
				command = "L"
			case sfnt.SegmentOpQuadTo:
				command = "Q"
				count = 2
			case sfnt.SegmentOpCubeTo:
				command = "C"
				count = 3
			}
			path.WriteString(command)
			for i := 0; i < count; i++ {
				if i > 0 {
					path.WriteByte(' ')
				}
				path.WriteString(strconv.FormatFloat(x+float64(segment.Args[i].X)/64, 'f', 3, 64))
				path.WriteByte(' ')
				path.WriteString(strconv.FormatFloat(float64(segment.Args[i].Y)/64, 'f', 3, 64))
			}
		}
		advance, err := face.GlyphAdvance(&buffer, glyph, ppem, font.HintingNone)
		if err != nil {
			return "", 0, err
		}
		x += float64(advance) / 64
		if path.Len() > 2<<20 {
			return "", 0, errors.New("text too complex")
		}
	}
	return path.String(), x, nil
}
