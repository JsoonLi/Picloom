package studio

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/generation"
)

func (a *App) requestSize() string {
	if a.outputFormat == "gif" {
		return a.gifSize
	}
	return computeSize(a.ratio, a.resolution)
}

func (a *App) gifSizeControl(c *ui.Context, narrow bool) {
	labels := []string{}
	sizes := map[string]string{}
	for _, size := range generation.GIFSizes {
		if generation.GIFFrameLimit(size) < a.gifFrameCount {
			continue
		}
		label := strings.ReplaceAll(size, "x", " × ") + " / 帧"
		labels = append(labels, label)
		sizes[label] = size
	}
	current := strings.ReplaceAll(a.gifSize, "x", " × ") + " / 帧"
	width := float32(192)
	if narrow {
		width = 160
	}
	if ui.Select(c.Key("gif-size"), &current, labels).Label("GIF 单帧尺寸").Width(width).MaxWidthPercent(100).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() {
		a.gifSize = sizes[current]
		a.savePreferences()
	}
}

func (a *App) gifTimingControls(c *ui.Context) {
	ui.Row(c.Key("gif-animation-controls")).Gap(8).Wrap().MaxWidthPercent(100).Children(func() {
		limit := generation.GIFFrameLimit(a.gifSize)
		counts := []int{}
		for _, n := range []int{2, 4, 6, 8, 9, 12, 16, 24, 32, 36, 48, 64} {
			if n <= limit {
				counts = append(counts, n)
			}
		}
		if limit >= 2 && !intContains(counts, limit) {
			counts = append(counts, limit)
		}
		if a.gifFrameCount >= 2 && a.gifFrameCount <= limit && !intContains(counts, a.gifFrameCount) {
			counts = append(counts, a.gifFrameCount)
		}
		sort.Ints(counts)
		options := []string{}
		values := map[string]int{}
		for _, n := range counts {
			label := strconv.Itoa(n) + " 帧"
			options = append(options, label)
			values[label] = n
		}
		selected := strconv.Itoa(a.gifFrameCount) + " 帧"
		if ui.Select(c.Key("gif-frame-count"), &selected, options).Label("GIF 帧数").Tooltip(fmt.Sprintf("当前单帧尺寸最多 %d 帧；缩小单帧尺寸可选择更多帧。", limit)).Width(96).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() {
			a.gifFrameCount = values[selected]
			a.savePreferences()
		}
		rates := []string{}
		durations := map[string]int{}
		for _, duration := range []int{2000, 1000, 500, 200, 100, 80, 50, 40, 20} {
			label := fmt.Sprintf("%g 帧/秒", 1000/float64(duration))
			rates = append(rates, label)
			durations[label] = duration
		}
		rate := fmt.Sprintf("%g 帧/秒", 1000/float64(a.gifFrameDurationMS))
		if ui.Select(c.Key("gif-frame-rate"), &rate, rates).Label("GIF 帧率").Tooltip("按此速度保存 GIF，保留渠道返回的全部画面和帧数。").Width(136).MinWidth(0).Height(composerControlHeight).Padding(4, 12).Radius(8).Changed() {
			a.gifFrameDurationMS = durations[rate]
			a.savePreferences()
		}
	})
}

func intContains(values []int, value int) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func (a *App) gifOptionsProblem() string {
	if a.outputFormat != "gif" {
		return ""
	}
	if len(a.references) > 0 {
		return "GIF 目前仅支持文生图，请移除参考图后生成。"
	}
	if err := generation.ValidateGIFSettings(a.gifSize, a.gifFrameCount, a.gifFrameDurationMS); err != nil {
		return err.Error()
	}
	return ""
}
