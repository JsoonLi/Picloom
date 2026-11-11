package generation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestGIFReferencesReachEditsWithAnimationSettings(t *testing.T) {
	for _, count := range []int{1, 4} {
		t.Run(fmt.Sprintf("references-%d", count), func(t *testing.T) {
			calls := 0
			service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/images/edits" {
					t.Errorf("wrong endpoint: %s", r.URL.Path)
				}
				if err := r.ParseMultipartForm(4 << 20); err != nil {
					t.Error(err)
					http.Error(w, "invalid multipart", http.StatusBadRequest)
					return
				}
				defer r.MultipartForm.RemoveAll()
				for key, want := range map[string]string{
					"model": baseArgs().Model, "prompt": baseArgs().Prompt,
					"size": "768x768", "n": "1", "output_format": "gif",
					"frame_count": "12", "frame_duration_ms": "100",
				} {
					if got := r.FormValue(key); got != want {
						t.Errorf("%s = %q, want %q", key, got, want)
					}
				}
				if _, exists := r.MultipartForm.Value["response_format"]; exists {
					t.Error("GIF edits must omit legacy response_format like GIF generation")
				}
				files := append(r.MultipartForm.File["image"], r.MultipartForm.File["image[]"]...)
				if len(files) != count {
					t.Errorf("got %d references, want %d", len(files), count)
				}
				for _, file := range files {
					f, err := file.Open()
					if err != nil {
						t.Error(err)
						continue
					}
					data, err := io.ReadAll(f)
					f.Close()
					if err != nil || !bytes.Equal(data, fixturePNG()) || file.Header.Get("Content-Type") != "image/png" {
						t.Error("reference bytes or MIME type changed")
					}
				}
				json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(fixtureGIF())}}})
			})
			args := baseArgs()
			args.OutputFormat, args.Size = "gif", "768x768"
			args.FrameCount, args.FrameDurationMS = 12, 100
			for range count {
				args.ReferenceImages = append(args.ReferenceImages, DataURL(fixturePNG(), "image/png"))
			}
			record, err := service.Generate(context.Background(), args, nil)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || record.ReferenceCount != count || len(record.ReferencePaths) != count || record.MIMEType != "image/gif" || record.GIFFrameCount != 12 || record.GIFFrameDurationMS != 100 || len(store.rows) != 1 {
				t.Fatalf("GIF result or reference history lost: %+v, calls=%d", record, calls)
			}
		})
	}
}
