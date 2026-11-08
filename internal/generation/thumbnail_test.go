package generation

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestThumbnailFitsBoundsAndKeepsProportions(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 3000, 1000))); err != nil {
		t.Fatal(err)
	}
	img, err := Thumbnail(data.Bytes(), 312, 960)
	if err != nil {
		t.Fatal(err)
	}
	if size := img.Bounds().Size(); size != image.Pt(312, 104) {
		t.Fatalf("thumbnail must fit the bounds at the original ratio, got %v", size)
	}
	if _, err := Thumbnail(data.Bytes(), 0, MaxThumbnailSide+1); err == nil {
		t.Fatal("invalid thumbnail bounds must be rejected")
	}
}

func TestEncodeThumbnailKeepsTransparency(t *testing.T) {
	opaque := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := 3; i < len(opaque.Pix); i += 4 {
		opaque.Pix[i] = 0xff
	}
	for _, test := range []struct {
		img    image.Image
		format string
	}{{opaque, "jpeg"}, {image.NewNRGBA(image.Rect(0, 0, 4, 4)), "png"}} {
		data, err := EncodeThumbnail(test.img)
		if err != nil {
			t.Fatal(err)
		}
		if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err != nil || format != test.format {
			t.Fatalf("want %s thumbnail, got %s (%v)", test.format, format, err)
		}
	}
}

func TestBoxReduceAveragesEveryPixelPremultiplied(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 5, 2))
	for x := range 5 {
		source.SetNRGBA(x, 0, color.NRGBA{255, 0, 0, 255})
		source.SetNRGBA(x, 1, color.NRGBA{255, 255, 255, 0})
	}
	out := boxReduce(source, 2)
	if out.Bounds().Dx() != 3 || out.Bounds().Dy() != 1 {
		t.Fatalf("partial edge blocks must be kept, got %v", out.Bounds())
	}
	for x := range 3 {
		// Transparent white contributes nothing to colour once premultiplied.
		if got := out.RGBAAt(x, 0); got != (color.RGBA{128, 0, 0, 128}) {
			t.Fatalf("pixel %d = %v", x, got)
		}
	}
}

func TestDownscaleReadsYCbCrWithoutAliasing(t *testing.T) {
	source := image.NewYCbCr(image.Rect(0, 0, 64, 64), image.YCbCrSubsampleRatio420)
	for y := range 64 {
		for x := range 64 {
			if (x+y)%2 == 0 {
				source.Y[source.YOffset(x, y)] = 255
			}
		}
	}
	for i := range source.Cb {
		source.Cb[i], source.Cr[i] = 128, 128
	}
	out := downscale(source, 8, 8)
	for i := 0; i < len(out.Pix); i += 4 {
		if v := out.Pix[i]; v < 120 || v > 135 {
			t.Fatalf("a fine checkerboard must average to grey, got %d", v)
		}
	}
}
