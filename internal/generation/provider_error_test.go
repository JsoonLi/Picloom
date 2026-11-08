package generation

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProvider400ExplainsRejectedParameterWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "Unsupported output_format: gif. Supported values are png, jpeg, webp.", "code": "invalid_value", "param": "output_format"}})
	})
	args := baseArgs()
	args.OutputFormat = "gif"
	_, err := service.Generate(context.Background(), args, nil)
	if err == nil || !strings.Contains(err.Error(), "output_format") || !strings.Contains(err.Error(), "png, jpeg, webp") || !strings.Contains(err.Error(), "invalid_value") {
		t.Fatalf("provider reason lost: %v", err)
	}
	if calls.Load() != 1 || len(store.rows) != 0 {
		t.Fatalf("unexpected retry or saved result: calls=%d rows=%d", calls.Load(), len(store.rows))
	}
}

func TestProviderErrorDropsUnstructuredOrOversizedBodies(t *testing.T) {
	for _, body := range []string{"private unstructured gateway details", `<html>private gateway details</html>`, `{"error":{"message":"` + strings.Repeat("private-data", maxProviderErrorBytes) + `"}}`} {
		service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400); io.WriteString(w, body) })
		_, err := service.Generate(context.Background(), baseArgs(), nil)
		if err == nil || strings.Contains(err.Error(), "private") || !strings.Contains(err.Error(), "HTTP 400") {
			t.Fatal("unstructured response escaped sanitizer")
		}
	}
}

func TestProviderErrorBodyReadHonorsTimeout(t *testing.T) {
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	service.Timeout = 30 * time.Millisecond
	_, err := service.Generate(context.Background(), baseArgs(), nil)
	if err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("error body read lost timeout: %v", err)
	}
}

func TestProviderErrorRedactsCredentialsAndRequestContents(t *testing.T) {
	service, _, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "Invalid format. key=local-test-token prompt=a quiet studio URL=https://user:password@example.test/image?token=private-value\nAuthorization: Bearer secret-auth-value"}})
	})
	_, err := service.Generate(context.Background(), baseArgs(), nil)
	if err == nil || !strings.Contains(err.Error(), "Invalid format") {
		t.Fatalf("safe provider message missing: %v", err)
	}
	for _, secret := range []string{"local-test-token", "a quiet studio", "password", "private-value", "secret-auth-value", "\n"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("private data leaked in error: %q", secret)
		}
	}
}
