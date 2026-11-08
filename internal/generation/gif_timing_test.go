package generation

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func TestGIFFixedRatePreservesEveryFrameAndOriginalBytes(t *testing.T) {
	original := fixtureGIF()
	untouched := append([]byte(nil), original...)
	result, err := retimeGIF(original, 100)
	if err != nil {
		t.Fatal(err)
	}
	before, err := gif.DecodeAll(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	after, err := gif.DecodeAll(bytes.NewReader(result))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, untouched) || len(after.Image) != len(before.Image) || after.LoopCount != before.LoopCount {
		t.Fatal("GIF source, frames or loop changed")
	}
	for i := range before.Image {
		if after.Delay[i] != 10 || after.Image[i].Bounds() != before.Image[i].Bounds() || !bytes.Equal(after.Image[i].Pix, before.Image[i].Pix) {
			t.Fatalf("frame %d changed beyond delay", i)
		}
		if len(before.Disposal) > i && after.Disposal[i] != before.Disposal[i] {
			t.Fatal("disposal method changed")
		}
	}
}

func TestGIFFixedRateAddsMissingGraphicControls(t *testing.T) {
	palette := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	second.Pix[0] = 1
	var buffer bytes.Buffer
	if err := gif.EncodeAll(&buffer, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{0, 0}, LoopCount: -1}); err != nil {
		t.Fatal(err)
	}
	result, err := retimeGIF(buffer.Bytes(), 40)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(result))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Image) != 2 || decoded.Delay[0] != 4 || decoded.Delay[1] != 4 || decoded.LoopCount != -1 {
		t.Fatal("missing frame delays were not inserted correctly")
	}
}

func TestGIFFixedRateRejectsInvalidDataAndRates(t *testing.T) {
	for _, duration := range []int{0, 19, 2001} {
		if _, err := retimeGIF(fixtureGIF(), duration); err == nil {
			t.Fatal("invalid rate accepted")
		}
	}
	if _, err := retimeGIF([]byte("not a gif"), 100); err == nil {
		t.Fatal("invalid GIF accepted")
	}
}
