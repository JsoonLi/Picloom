package generation

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var Ratios = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}

func ComputeSize(ratio, resolution string) string {
	parts := strings.Split(ratio, ":")
	if len(parts) != 2 {
		return "1024x1024"
	}
	w, e1 := strconv.ParseFloat(parts[0], 64)
	h, e2 := strconv.ParseFloat(parts[1], 64)
	target := map[string]float64{"1k": 1024 * 1024, "2k": 2048 * 2048, "4k": 4096 * 4096}[resolution]
	if e1 != nil || e2 != nil || w <= 0 || h <= 0 || target == 0 || math.IsInf(w, 0) || math.IsInf(h, 0) || math.IsNaN(w) || math.IsNaN(h) {
		return "1024x1024"
	}
	round := func(v float64) int { return int(math.Max(256, math.Floor(v/16+0.5)*16)) }
	long := round(math.Sqrt(target * math.Max(w, h) / math.Min(w, h)))
	short := round(float64(long) * math.Min(w, h) / math.Max(w, h))
	if w >= h {
		return fmt.Sprintf("%dx%d", long, short)
	}
	return fmt.Sprintf("%dx%d", short, long)
}
func FindSizeCombo(size string) (string, string) {
	for _, r := range Ratios {
		for _, s := range []string{"2k", "4k"} {
			if ComputeSize(r, s) == size {
				return r, s
			}
		}
	}
	for _, r := range Ratios {
		if ComputeSize(r, "1k") == size {
			return r, "2k"
		}
	}
	return "3:4", "2k"
}
