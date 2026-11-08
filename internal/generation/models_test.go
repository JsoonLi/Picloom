package generation

import (
	"context"
	"image2ai/internal/domain"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func TestModelDiscoveryFilteringAndPricing(t *testing.T) {
	service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			if r.Header.Get("Authorization") != "Bearer local-test-token" {
				t.Error("model auth missing")
			}
			io.WriteString(w, `{"data":[{"id":"chat"},{"id":"fixture-image"},{"id":"fixture-image"},{"id":"multimodal","architecture":{"output_modalities":["image"]}}]}`)
		case "/models/pricing":
			if r.Header.Get("Authorization") != "" {
				t.Error("pricing must be public")
			}
			io.WriteString(w, `{"models":[{"id":"fixture-image","billing_mode":"per_image","list_cost":{"currency":"USD"},"per_call_tiers":[{"id":"2k","cost":0.0206,"list_cost":0.2},{"id":"4k","cost":0.0286},{"id":"8k","cost":5}]},{"id":"tokens","billing_mode":"tokens"}]}`)
		default:
			t.Error(r.URL.Path)
		}
	})
	models, err := service.DiscoverModels(context.Background(), store.config.Providers[0])
	if err != nil || !reflect.DeepEqual(models, []string{"fixture-image", "multimodal"}) {
		t.Fatal(models, err)
	}
	prices, err := service.DiscoverPricing(context.Background(), "local")
	if err != nil || len(prices) != 1 || prices[0].Prices["2k"] != 0.0206 || prices[0].Currency != "USD" {
		t.Fatal(prices, err)
	}
}
func TestMalformedDiscoveryAndUntrustedURLs(t *testing.T) {
	for _, payload := range []string{`{}`, `{"data":null,"models":null}`, `{"data":{},"models":{}}`, `not json`} {
		t.Run(payload, func(t *testing.T) {
			service, store, _ := setupService(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, payload) })
			if _, err := service.DiscoverModels(context.Background(), store.config.Providers[0]); err == nil {
				t.Fatal("accepted malformed models")
			}
			if _, err := service.DiscoverPricing(context.Background(), "local"); err == nil {
				t.Fatal("accepted malformed pricing")
			}
			if _, err := service.DiscoverModels(context.Background(), domain.Provider{BaseURL: "file:///secret"}); err == nil {
				t.Fatal("accepted file URL")
			}
		})
	}
}
