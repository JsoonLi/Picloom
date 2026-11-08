package studio

import (
	"time"

	"github.com/egoist/mygo/ui"
	"image2ai/internal/generation"
)

type previewAnimation struct {
	frames      []*ui.Bitmap
	delays      []time.Duration
	duration    time.Duration
	loops       int
	started     time.Time
	elapsed     time.Duration
	paused      bool
	initialized bool
}

func newPreviewAnimation(animation *generation.Animation) *previewAnimation {
	result := &previewAnimation{delays: animation.Delays, loops: animation.LoopCount}
	for _, frame := range animation.Frames {
		result.frames = append(result.frames, ui.NewBitmap(frame))
	}
	for _, delay := range result.delays {
		result.duration += delay
	}
	return result
}

func (a *previewAnimation) position(now time.Time) time.Duration {
	elapsed := a.elapsed
	if !a.paused {
		elapsed += max(time.Duration(0), now.Sub(a.started))
	}
	return elapsed
}

func (a *previewAnimation) frame(now time.Time) (int, time.Duration) {
	if a.duration <= 0 {
		return 0, 0
	}
	elapsed := a.position(now)
	if a.loops != 0 {
		plays := a.loops + 1
		if a.loops < 0 {
			plays = 1
		}
		if elapsed >= time.Duration(plays)*a.duration {
			return len(a.frames) - 1, 0
		}
	}
	at := elapsed % a.duration
	for i, delay := range a.delays {
		if at < delay {
			return i, delay - at
		}
		at -= delay
	}
	return len(a.frames) - 1, 0
}

func (a *App) animatedPreview(c *ui.Context) *ui.Bitmap {
	animation := a.previewAnimation
	if animation == nil {
		return a.previewBitmap
	}
	if !animation.initialized {
		animation.initialized = true
		animation.started = c.Now()
		animation.paused = c.Preferences().ReduceMotion
	}
	frame, next := animation.frame(c.Now())
	if !animation.paused && next > 0 {
		c.After(next)
	}
	return animation.frames[frame]
}

func (a *previewAnimation) toggle(now time.Time) {
	if a.paused {
		if _, next := a.frame(now); next == 0 {
			a.elapsed = 0
		}
		a.started = now
		a.paused = false
	} else {
		a.elapsed = a.position(now)
		a.paused = true
	}
}
