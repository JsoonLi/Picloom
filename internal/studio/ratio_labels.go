package studio

import "image2ai/internal/locale"

func ratioLabel(ratio string) string {
	switch ratio {
	case "1:1":
		return ratio + " " + locale.T("方形")
	case "2:3", "3:4", "4:5", "9:16":
		return ratio + " " + locale.T("竖版")
	case "3:2", "4:3", "5:4", "16:9", "21:9":
		return ratio + " " + locale.T("横版")
	default:
		return ratio
	}
}

func ratioLabels() []string {
	labels := make([]string, len(ratios))
	for i, ratio := range ratios {
		labels[i] = ratioLabel(ratio)
	}
	return labels
}
