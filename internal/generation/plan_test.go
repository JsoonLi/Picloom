package generation

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestPlanIsTheRequestGenerateSends compares the plan with what the provider
// receives, for a JSON and a multipart request.
func TestPlanIsTheRequestGenerateSends(t *testing.T) {
	for _, references := range []int{0, 2} {
		var gotPath, gotType string
		got := map[string]string{}
		gotFiles := map[string]string{}
		service, _, server := setupService(t, func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotType = r.URL.Path, r.Header.Get("Content-Type")
			if references == 0 {
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				for k, v := range body {
					b, _ := json.Marshal(v)
					var s string
					if json.Unmarshal(b, &s) != nil {
						s = string(b)
					}
					got[k] = s
				}
			} else {
				if err := r.ParseMultipartForm(4 << 20); err != nil {
					t.Error(err)
				}
				for k, v := range r.MultipartForm.Value {
					got[k] = v[0]
				}
				for k, files := range r.MultipartForm.File {
					gotFiles[k] = files[0].Filename
				}
			}
			sendPNG(w)
		})
		args := baseArgs()
		args.OutputFormat = "png"
		for i := 0; i < references; i++ {
			args.ReferenceImages = append(args.ReferenceImages, DataURL(fixturePNG(), "image/png"))
		}
		plan, err := service.Plan(args)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Generate(context.Background(), args, nil); err != nil {
			t.Fatal(err)
		}
		if plan.Method != "POST" || plan.URL != server.URL+gotPath || plan.APIKey != "****oken" {
			t.Fatalf("plan %+v, sent to %s", plan, gotPath)
		}
		if want := plan.ContentType; len(gotType) < len(want) || gotType[:len(want)] != want {
			t.Fatalf("content type %q, plan %q", gotType, want)
		}
		if len(plan.Fields) != len(got) {
			t.Fatalf("plan fields %v, sent %v", plan.Fields, got)
		}
		for _, f := range plan.Fields {
			if got[f.Name] != f.Value {
				t.Fatalf("field %s: plan %q, sent %q", f.Name, f.Value, got[f.Name])
			}
		}
		if len(plan.Images) != len(gotFiles) {
			t.Fatalf("plan images %v, sent %v", plan.Images, gotFiles)
		}
		for _, image := range plan.Images {
			if gotFiles[image.Field] != image.FileName || image.Width != 4 || image.Height != 3 || image.Bytes != len(fixturePNG()) {
				t.Fatalf("image %+v, sent %v", image, gotFiles)
			}
		}
	}
}

func TestPlanRejectsWhatGenerateRejectsAndMasksKeys(t *testing.T) {
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { t.Error("plan sent a request") })
	args := baseArgs()
	args.Size = "100x100"
	if _, err := service.Plan(args); err == nil {
		t.Fatal("invalid size planned")
	}
	for key, want := range map[string]string{"": "", "abc": "****", "sk-123456": "****3456"} {
		if got := maskKey(key); got != want {
			t.Fatalf("maskKey(%q) = %q, want %q", key, got, want)
		}
	}
}
