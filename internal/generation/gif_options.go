package generation

import (
	"errors"
	"image2ai/internal/locale"
	"strconv"
	"strings"
)

const DefaultGIFFrameCount = 9
const DefaultGIFFrameDurationMS = 200
const DefaultGIFSize = "1024x1024"

var GIFSizes = []string{"512x512", "768x768", "1024x1024", "512x768", "768x512", "768x1024", "1024x768", "576x1024", "1024x576"}

func NormalizeGIFTiming(frames, duration int) (int, int, error) {
	if frames == 0 {
		frames = DefaultGIFFrameCount
	}
	if duration == 0 {
		duration = DefaultGIFFrameDurationMS
	}
	if frames < 2 || frames > 64 || duration < 20 || duration > 2000 {
		return 0, 0, errors.New(locale.T("GIF 帧数应为 2–64，每帧时长应为 20–2000 毫秒。"))
	}
	return frames, duration, nil
}

func GIFFrameLimit(size string) int {
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return 0
	}
	w, e1 := strconv.Atoi(parts[0])
	h, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || w < 256 || h < 256 || w > 8192 || h > 8192 || w%16 != 0 || h%16 != 0 {
		return 0
	}
	return min(64, int(int64(maxGIFPixels)/(int64(w)*int64(h))))
}

func ValidateGIFSettings(size string, frames, duration int) error {
	if size == "" {
		size = DefaultGIFSize
	}
	frames, _, err := NormalizeGIFTiming(frames, duration)
	if err != nil {
		return err
	}
	if frames > GIFFrameLimit(size) {
		return errors.New(locale.T("GIF 单帧尺寸与帧数组合过大，请缩小单帧尺寸或减少帧数。"))
	}
	return nil
}
