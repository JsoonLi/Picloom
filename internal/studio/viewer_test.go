package studio

import (
	"math"
	"testing"
)

func TestViewerFitsZoomsAtDetailAndConstrainsPanning(t *testing.T) {
	var view imageView
	view.reset()
	if scale := fitImage(800, 600, 4000, 3000); math.Abs(float64(scale)-0.2) > 0.0001 {
		t.Fatalf("fit scale %v", scale)
	}
	view.zoom(0.4, 200, 0, 800, 600, 4000, 3000)
	if view.fitted || view.scale != 0.4 || view.x != -200 || view.y != 0 {
		t.Fatalf("detail anchor moved: %+v", view)
	}
	view.zoom(100, 0, 0, 800, 600, 4000, 3000)
	if view.scale != 8 {
		t.Fatal("maximum zoom not enforced")
	}
	view.x, view.y = 1e9, -1e9
	view.constrain(800, 600, 4000, 3000)
	if view.x != 15600 || view.y != -11700 {
		t.Fatalf("panning escaped picture: %+v", view)
	}
	view.zoom(0.0001, 0, 0, 800, 600, 4000, 3000)
	if view.scale != 0.1 || view.x != 0 || view.y != 0 {
		t.Fatalf("minimum zoom did not center: %+v", view)
	}
	view.reset()
	if !view.fitted || view.x != 0 || view.y != 0 {
		t.Fatal("fit failed to reset viewport")
	}
}

func TestParameterSizesKeepLegacyPixelMath(t *testing.T) {
	for _, test := range []struct{ ratio, resolution, want string }{{"3:4", "2k", "1776x2368"}, {"3:4", "4k", "3552x4736"}, {"1:1", "2k", "2048x2048"}, {"16:9", "2k", "2736x1536"}} {
		if got := computeSize(test.ratio, test.resolution); got != test.want {
			t.Errorf("%s %s: %s != %s", test.ratio, test.resolution, got, test.want)
		}
	}
}
