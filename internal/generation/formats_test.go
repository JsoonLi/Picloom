package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureGIF() []byte {
	p := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	a := image.NewPaletted(image.Rect(0, 0, 4, 3), p)
	b := image.NewPaletted(image.Rect(0, 0, 4, 3), p)
	a.SetColorIndex(0, 0, 1)
	b.SetColorIndex(0, 0, 2)
	var out bytes.Buffer
	_ = gif.EncodeAll(&out, &gif.GIF{Image: []*image.Paletted{a, b}, Delay: []int{5, 10}, LoopCount: 0})
	return out.Bytes()
}

func TestGIFRequestUsesDocumentedAnimationDefaults(t *testing.T) {
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if _, exists := request["response_format"]; exists {
			t.Error("GIF must use the documented request without legacy response_format")
		}
		if request["frame_count"] != float64(9) || request["frame_duration_ms"] != float64(200) {
			t.Errorf("GIF frame parameters missing: count=%v duration=%v", request["frame_count"], request["frame_duration_ms"])
		}
		if request["prompt"] != baseArgs().Prompt {
			t.Error("prompt changed")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(fixtureGIF())}}})
	})
	args := baseArgs()
	args.OutputFormat = "gif"
	if _, err := service.Generate(context.Background(), args, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOutputFormatIsSentForGenerationAndEdits(t *testing.T) {
	for _, edit := range []bool{false, true} {
		service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
			if edit {
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
				}
				if r.MultipartForm != nil {
					defer r.MultipartForm.RemoveAll()
				}
				if r.FormValue("output_format") != "png" {
					t.Error("edit format missing")
				}
			} else {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if body["output_format"] != "png" {
					t.Error("generation format missing")
				}
			}
			sendPNG(w)
		})
		args := baseArgs()
		args.OutputFormat = "png"
		if edit {
			args.ReferenceImages = []string{DataURL(fixturePNG(), "image/png")}
		}
		if _, err := service.Generate(context.Background(), args, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRequestedFormatMismatchDoesNotSaveMislabelledImage(t *testing.T) {
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { sendPNG(w) })
	args := baseArgs()
	args.OutputFormat = "gif"
	if _, err := service.Generate(context.Background(), args, nil); err == nil || !strings.Contains(err.Error(), "未返回所选") {
		t.Fatal(err)
	}
	if len(store.rows) != 0 {
		t.Fatal("mismatched format saved")
	}
}

func TestUnsafeSVGAndOversizedAnimationAreRejectedBeforeDecoding(t *testing.T) {
	for _, svg := range []string{
		`<svg width="10" height="10"><defs><g id="loop"><use href="#loop"/></g></defs><use href="#loop"/></svg>`,
		`<svg width="10" height="10"><script>alert(1)</script></svg>`,
		`<svg width="10" height="10"><image href="file:///private/file.png"/></svg>`,
		`<svg width="10" height="10"><use href="https://example.test/file.svg#x"/></svg>`,
		`<svg width="10" height="10"><style>@import 'https://example.test/a.css';</style></svg>`,
		`<svg width="NaN" height="10"/>`,
		`<svg width="1000000" height="1000000"/>`,
		`<!DOCTYPE svg [<!ENTITY a SYSTEM "file:///private/file">]><svg width="10" height="10"/>`,
	} {
		if _, _, err := svgSize([]byte(svg)); err == nil {
			t.Fatal("unsafe SVG accepted")
		}
	}
	data := fixtureGIF()
	binary.LittleEndian.PutUint16(data[6:8], 65535)
	binary.LittleEndian.PutUint16(data[8:10], 65535)
	if _, _, _, err := decodeImage(data); err == nil {
		t.Fatal("oversized GIF accepted")
	}
	if _, _, _, err := decodeImage(fixtureGIF()[:len(fixtureGIF())-2]); err == nil {
		t.Fatal("truncated GIF accepted")
	}
}

func TestSVGDataURLAndVectorThumbnail(t *testing.T) {
	data, err := resolveImage(context.Background(), map[string]any{"data": []any{map[string]any{"url": DataURL(fixtureSVG, "image/svg+xml")}}}, http.DefaultClient)
	if err != nil || !bytes.Equal(data, fixtureSVG) {
		t.Fatal(err)
	}
	img, err := Thumbnail(data, 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(5, 5).RGBA()
	if r < 60000 || g != 0 || b != 0 || a < 60000 {
		t.Fatalf("vector failed to render: %d %d %d %d", r, g, b, a)
	}
}

func TestGIFFramesRetainTimingAndColors(t *testing.T) {
	animation, err := DecodeAnimation(fixtureGIF())
	if err != nil {
		t.Fatal(err)
	}
	if len(animation.Frames) != 2 || animation.Delays[0].Milliseconds() != 50 || animation.Delays[1].Milliseconds() != 100 {
		t.Fatal(animation)
	}
	r, _, _, _ := animation.Frames[0].At(0, 0).RGBA()
	_, _, b, _ := animation.Frames[1].At(0, 0).RGBA()
	if r != 65535 || b != 65535 {
		t.Fatal("animation frames lost")
	}
}

func TestGIFDisposalCompositesPartialFrames(t *testing.T) {
	palette := color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	full := image.NewPaletted(image.Rect(0, 0, 2, 1), palette)
	full.Pix[0] = 1
	full.Pix[1] = 1
	left := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	left.Pix[0] = 2
	right := image.NewPaletted(image.Rect(1, 0, 2, 1), palette)
	right.Pix[0] = 2
	last := image.NewPaletted(image.Rect(0, 0, 1, 1), palette)
	var data bytes.Buffer
	err := gif.EncodeAll(&data, &gif.GIF{Image: []*image.Paletted{full, left, right, last}, Delay: []int{5, 5, 5, 5}, Disposal: []byte{gif.DisposalNone, gif.DisposalPrevious, gif.DisposalBackground, gif.DisposalNone}})
	if err != nil {
		t.Fatal(err)
	}
	animation, err := DecodeAnimation(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ := animation.Frames[2].At(0, 0).RGBA()
	_, _, b, _ := animation.Frames[2].At(1, 0).RGBA()
	_, _, _, alpha := animation.Frames[3].At(1, 0).RGBA()
	if r != 65535 || b != 65535 || alpha != 0 {
		t.Fatalf("GIF disposal failed: r=%d b=%d alpha=%d", r, b, alpha)
	}
}

func TestSVGTextCanRenderWithDefaultFont(t *testing.T) {
	data := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="40"><text x="2" y="28" font-size="24">SVG</text></svg>`)
	img, err := Thumbnail(data, 120, 40)
	if err != nil {
		t.Fatal(err)
	}
	visible := false
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0 {
				visible = true
			}
		}
	}
	if !visible {
		t.Fatal("SVG text disappeared")
	}
}

var fixtureSVG = []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="30" viewBox="0 0 40 30"><rect width="40" height="30" fill="red"/></svg>`)

func TestGeneratedGIFAndSVGArePreserved(t *testing.T) {
	for _, tc := range []struct {
		name, mime string
		data       []byte
		raw        bool
	}{
		{"gif", "image/gif", fixtureGIF(), false},
		{"svg", "image/svg+xml", fixtureSVG, false},
		{"raw-svg", "image/svg+xml", fixtureSVG, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.raw {
					w.Header().Set("Content-Type", tc.mime)
					w.Write(tc.data)
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(tc.data)}}})
			})
			record, err := service.Generate(context.Background(), baseArgs(), nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(record.ImagePath)
			if err != nil || !bytes.Equal(data, tc.data) || record.MIMEType != tc.mime {
				t.Fatalf("format or original bytes lost: %+v %v", record, err)
			}
			ext := ".svg"
			if tc.mime == "image/gif" {
				ext = ".gif"
			}
			if filepath.Ext(record.ImagePath) != ext {
				t.Fatal(record.ImagePath)
			}
			thumb, err := Thumbnail(data, 20, 20)
			if err != nil || thumb.Bounds().Dx() > 20 || thumb.Bounds().Dy() > 20 {
				t.Fatalf("thumbnail: %v", err)
			}
		})
	}
}
