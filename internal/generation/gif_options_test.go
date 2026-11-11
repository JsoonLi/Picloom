package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/gif"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
)

func TestGIFCustomTimingReachesProviderAndHistory(t *testing.T) {
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["frame_count"] != float64(12) || payload["frame_duration_ms"] != float64(100) || payload["size"] != "768x768" {
			t.Errorf("animation request changed: %v", payload)
		}
		if payload["prompt"] != baseArgs().Prompt {
			t.Error("prompt changed")
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(fixtureGIF())}}})
	})
	args := baseArgs()
	args.OutputFormat = "gif"
	args.FrameCount = 12
	args.FrameDurationMS = 100
	args.Size = "768x768"
	record, err := service.Generate(context.Background(), args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.GIFFrameCount != 12 || record.GIFFrameDurationMS != 100 || len(store.rows) != 1 || store.rows[0].GIFFrameDurationMS != 100 {
		t.Fatal("requested animation settings lost")
	}
	data, e := os.ReadFile(record.ImagePath)
	if e != nil {
		t.Fatal(e)
	}
	animation, e := gif.DecodeAll(bytes.NewReader(data))
	if e != nil || animation.Delay[0] != 10 || animation.Delay[1] != 10 {
		t.Fatal("saved GIF did not use chosen frame rate")
	}
}

func TestInvalidGIFOptionsNeverReachPaidProvider(t *testing.T) {
	var calls atomic.Int32
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); sendPNG(w) })
	for _, tc := range []struct {
		frames, duration int
		size             string
		reference        bool
	}{{1, 200, "1024x1024", false}, {65, 200, "512x512", false}, {9, 19, "1024x1024", false}, {9, 2001, "1024x1024", false}, {64, 200, "1024x1024", false}, {9, 200, "1776x2368", false}, {64, 200, "1024x1024", true}, {9, 19, "1024x1024", true}} {
		args := baseArgs()
		args.OutputFormat = "gif"
		args.FrameCount = tc.frames
		args.FrameDurationMS = tc.duration
		args.Size = tc.size
		if tc.reference {
			args.ReferenceImages = []string{DataURL(fixturePNG(), "image/png")}
		}
		if _, err := service.Generate(context.Background(), args, nil); err == nil {
			t.Errorf("invalid GIF request accepted: %+v", tc)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid animation request reached provider")
	}
	if GIFFrameLimit("512x512") != 64 || GIFFrameLimit("1024x1024") != 16 {
		t.Fatal("size and frame budget mismatch")
	}
}

func TestStaticImagesDoNotReceiveGIFTiming(t *testing.T) {
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["response_format"] != "b64_json" || payload["frame_count"] != nil || payload["frame_duration_ms"] != nil {
			t.Error("static image request changed")
		}
		sendPNG(w)
	})
	args := baseArgs()
	args.FrameCount = 12
	args.FrameDurationMS = 100
	if _, err := service.Generate(context.Background(), args, nil); err != nil {
		t.Fatal(err)
	}
}
