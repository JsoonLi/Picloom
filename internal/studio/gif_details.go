package studio

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

func artworkSizeLabel(record domain.ImageRecord) string {
	if record.MIMEType == "image/gif" {
		return fmt.Sprintf("%d × %d px", record.Width, record.Height)
	}
	return record.Size
}

func (a *App) gifActualTiming(c *ui.Context, record domain.ImageRecord) {
	animation := a.previewAnimation
	if record.MIMEType != "image/gif" || animation == nil || animation.duration <= 0 {
		return
	}
	ui.Text(c.Key("gif-actual-timing"), fmt.Sprintf("实际 %d 帧 · 平均 %.1f 帧/秒", len(animation.frames), float64(len(animation.frames))/animation.duration.Seconds())).FontSize(textSize(c, 12)).TextColor(c.Theme().TextMuted)
}

func (a *App) reuseGIFSettings(record domain.ImageRecord) {
	frames, duration, err := generation.NormalizeGIFTiming(record.GIFFrameCount, record.GIFFrameDurationMS)
	if err != nil {
		a.problem = err.Error()
		return
	}
	a.gifFrameCount, a.gifFrameDurationMS = frames, duration
	a.gifSize = generation.DefaultGIFSize
	if contains(generation.GIFSizes, record.Size) && generation.ValidateGIFSettings(record.Size, frames, duration) == nil {
		a.gifSize = record.Size
		return
	}
	if generation.ValidateGIFSettings(a.gifSize, frames, duration) != nil {
		a.gifSize = "512x512"
	}
}
