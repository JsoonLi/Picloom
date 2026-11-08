package generation

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func markupPNG(t *testing.T, size image.Point) []byte {
	t.Helper()
	data, err := RenderMarkupPNG(size, []MarkupStroke{{Tool: "pen", Points: [][2]float32{{1, 1}}, Width: 1}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMarkupPNGKeepsBackgroundTransparentAndShapesAligned(t *testing.T) {
	data, err := RenderMarkupPNG(image.Pt(128, 96), []MarkupStroke{
		{Tool: "rectangle", Points: [][2]float32{{10, 20}, {40, 60}}, Width: 4},
		{Tool: "ellipse", Points: [][2]float32{{70, 10}, {110, 60}}, Width: 4},
		{Tool: "pen", Points: [][2]float32{{10, 80}, {50, 80}}, Width: 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Size() != image.Pt(128, 96) {
		t.Fatalf("overlay size: %v", err)
	}
	for _, p := range []image.Point{{0, 0}, {25, 40}, {90, 35}, {127, 95}} {
		if _, _, _, alpha := img.At(p.X, p.Y).RGBA(); alpha != 0 {
			t.Fatalf("background/shape interior must be transparent at %v", p)
		}
	}
	for _, p := range []image.Point{{10, 40}, {25, 20}, {90, 10}, {110, 35}, {30, 80}} {
		if got := color.NRGBAModel.Convert(img.At(p.X, p.Y)); got != MarkupColor {
			t.Fatalf("missing red annotation at %v: %v", p, got)
		}
	}
}

func TestMarkupMultipartSeparatesSourceAndOverlay(t *testing.T) {
	original := fixturePNG()
	overlay := markupPNG(t, image.Pt(4, 3))
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Error(r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		for key, expected := range map[string][]byte{"image": original, "markup": overlay} {
			files := r.MultipartForm.File[key]
			if len(files) != 1 || files[0].Header.Get("Content-Type") != "image/png" {
				t.Errorf("%s: %v", key, files)
				continue
			}
			f, _ := files[0].Open()
			data, _ := io.ReadAll(f)
			f.Close()
			if !bytes.Equal(data, expected) {
				t.Errorf("%s pixels changed or composited", key)
			}
		}
		if len(r.MultipartForm.File) != 2 || r.FormValue("size") != "auto" || r.FormValue("prompt") != "replace the marked area" {
			t.Error("unexpected edit request")
		}
		sendPNG(w)
	})
	args := baseArgs()
	args.Prompt, args.Size = "replace the marked area", "auto"
	args.ReferenceImages = []string{DataURL(original, "image/png")}
	args.Markup = DataURL(overlay, "image/png")
	record, err := service.Generate(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.ReferenceCount != 1 || len(record.ReferencePaths) != 1 {
		t.Fatal("overlay must not count as a reference")
	}
	saved, _ := os.ReadFile(record.ReferencePaths[0])
	if !bytes.Equal(saved, original) {
		t.Fatal("original reference changed")
	}
}

func TestMarkupRejectsInvalidEditsBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); sendPNG(w) })
	base := baseArgs()
	base.ReferenceImages = []string{DataURL(fixturePNG(), "image/png")}
	base.Markup = DataURL(markupPNG(t, image.Pt(4, 3)), "image/png")
	opaque := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for i := range opaque.Pix {
		opaque.Pix[i] = 255
	}
	var solid, empty bytes.Buffer
	_ = png.Encode(&solid, opaque)
	_ = png.Encode(&empty, image.NewNRGBA(opaque.Bounds()))
	for _, tc := range []struct {
		name, markup, format string
		refs                 []string
	}{
		{"no source", base.Markup, "", nil},
		{"two sources", base.Markup, "", append(base.ReferenceImages, base.ReferenceImages[0])},
		{"gif output", base.Markup, "gif", base.ReferenceImages},
		{"svg output", base.Markup, "svg", base.ReferenceImages},
		{"wrong dimensions", DataURL(markupPNG(t, image.Pt(5, 3)), "image/png"), "", base.ReferenceImages},
		{"opaque overlay", DataURL(solid.Bytes(), "image/png"), "", base.ReferenceImages},
		{"empty overlay", DataURL(empty.Bytes(), "image/png"), "", base.ReferenceImages},
		{"wrong format", DataURL(fixturePNG(), "image/jpeg"), "", base.ReferenceImages},
		{"truncated", DataURL(solid.Bytes()[:len(solid.Bytes())-12], "image/png"), "", base.ReferenceImages},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := base
			args.Markup, args.OutputFormat, args.ReferenceImages = tc.markup, tc.format, tc.refs
			if _, err := service.Generate(context.Background(), args, nil); err == nil {
				t.Fatal("invalid edit accepted")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("invalid edits reached provider")
	}
	for _, bad := range []MarkupStroke{{Tool: "pen", Points: [][2]float32{{1, 1}}, Width: float32(math.NaN())}, {Tool: "pen", Points: [][2]float32{{-1, 1}}, Width: 1}} {
		if _, err := RenderMarkupPNG(image.Pt(4, 3), []MarkupStroke{bad}); err == nil {
			t.Fatal("unsafe geometry accepted")
		}
	}
}

func TestMarkupExplicitOutputSizeKeepsOverlayAtSourceSize(t *testing.T) {
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("size") != "2048x2048" {
			t.Error("chosen output size was not sent")
		}
		for _, name := range []string{"image", "markup"} {
			f, _, err := r.FormFile(name)
			if err != nil {
				t.Error(err)
				continue
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				t.Error(err)
				continue
			}
			size, err := ImageSize(data)
			if err != nil || size != image.Pt(4, 3) {
				t.Errorf("%s must keep source dimensions, got %v: %v", name, size, err)
			}
		}
		sendPNG(w)
	})
	args := baseArgs()
	args.Size = "2048x2048"
	args.ReferenceImages = []string{DataURL(fixturePNG(), "image/png")}
	args.Markup = DataURL(markupPNG(t, image.Pt(4, 3)), "image/png")
	if _, err := service.Generate(context.Background(), args, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMarkupSourceKeepsJPEGOrientationAligned(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			source.Set(x, y, color.NRGBA{R: uint8(x * 14), G: uint8(y * 28), B: 100, A: 255})
		}
	}
	var jpg bytes.Buffer
	_ = jpeg.Encode(&jpg, source, &jpeg.Options{Quality: 100})
	decoded, _ := jpeg.Decode(bytes.NewReader(jpg.Bytes()))
	for _, tc := range []struct {
		orientation   uint16
		size, topLeft image.Point
	}{{2, image.Pt(16, 8), image.Pt(15, 0)}, {6, image.Pt(8, 16), image.Pt(0, 7)}, {8, image.Pt(8, 16), image.Pt(15, 0)}} {
		tiff := make([]byte, 26)
		copy(tiff, "II")
		binary.LittleEndian.PutUint16(tiff[2:], 42)
		binary.LittleEndian.PutUint32(tiff[4:], 8)
		binary.LittleEndian.PutUint16(tiff[8:], 1)
		binary.LittleEndian.PutUint16(tiff[10:], 0x112)
		binary.LittleEndian.PutUint16(tiff[12:], 3)
		binary.LittleEndian.PutUint32(tiff[14:], 1)
		binary.LittleEndian.PutUint16(tiff[18:], tc.orientation)
		segment := append([]byte("Exif\x00\x00"), tiff...)
		withExif := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(segment) + 2)}, segment...)
		withExif = append(withExif, jpg.Bytes()[2:]...)
		data, mime, size, err := PrepareMarkupSource(withExif)
		if err != nil || mime != "image/png" || size != tc.size {
			t.Fatalf("orientation %d: %s %v %v", tc.orientation, mime, size, err)
		}
		result, _ := png.Decode(bytes.NewReader(data))
		if color.NRGBAModel.Convert(result.At(0, 0)) != color.NRGBAModel.Convert(decoded.At(tc.topLeft.X, tc.topLeft.Y)) {
			t.Errorf("orientation %d moved the wrong pixels", tc.orientation)
		}
	}
	unchanged, _, _, err := PrepareMarkupSource(fixturePNG())
	if err != nil || !bytes.Equal(unchanged, fixturePNG()) {
		t.Fatal("PNG source changed")
	}
	if _, _, _, err = PrepareMarkupSource([]byte(strings.Repeat("x", 64))); err == nil {
		t.Fatal("invalid source accepted")
	}
}
