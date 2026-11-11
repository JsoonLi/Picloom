package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image2ai/internal/domain"
	"image2ai/internal/locale"
	"image2ai/internal/network"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf16"
)

func (s *Service) getDiscovery(ctx context.Context, provider domain.Provider, path string, auth bool) ([]byte, error) {
	base := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if _, err := validHTTP(base); err != nil {
		return nil, err
	}
	config, err := s.store.ReadConfig()
	if err != nil {
		return nil, err
	}
	client, err := network.NewClient(config.Proxy, 15*time.Second)
	if err != nil {
		return nil, err
	}
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, errors.New(locale.T("模型服务地址无效。"))
	}
	if key := strings.TrimSpace(provider.APIKey); auth && key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New(locale.T("获取模型或价格失败，请检查服务和网络。"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf(locale.T("获取模型或价格失败（HTTP %d）。"), response.StatusCode)
	}
	return readLimited(response, 2*1024*1024)
}
func (s *Service) DiscoverModels(ctx context.Context, provider domain.Provider) ([]string, error) {
	raw, err := s.getDiscovery(ctx, provider, "/models", true)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Data) == 0 || string(payload.Data) == "null" {
		return nil, errors.New(locale.T("服务返回的模型列表格式无效。"))
	}
	var entries []struct {
		ID           string   `json:"id"`
		Output       []string `json:"output_modalities"`
		Architecture struct {
			Output []string `json:"output_modalities"`
		} `json:"architecture"`
	}
	if json.Unmarshal(payload.Data, &entries) != nil {
		return nil, errors.New(locale.T("服务返回的模型列表格式无效。"))
	}
	models := []string{}
	for _, entry := range entries {
		if entry.ID == "" || len(utf16.Encode([]rune(entry.ID))) > 200 {
			continue
		}
		if slices.Contains(entry.Output, "image") || slices.Contains(entry.Architecture.Output, "image") || imageModelPattern.MatchString(entry.ID) {
			if !slices.Contains(models, entry.ID) {
				models = append(models, entry.ID)
			}
		}
	}
	slices.Sort(models)
	return models, nil
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func (s *Service) DiscoverPricing(ctx context.Context, providerID string) ([]domain.ModelPricing, error) {
	config, err := s.store.ReadConfig()
	if err != nil {
		return nil, err
	}
	var provider domain.Provider
	for _, p := range config.Providers {
		if p.ID == providerID {
			provider = p
			break
		}
	}
	if provider.ID == "" {
		return nil, errors.New(locale.T("提供商不存在。"))
	}
	raw, err := s.getDiscovery(ctx, provider, "/models/pricing", false)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Models json.RawMessage `json:"models"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Models) == 0 || string(payload.Models) == "null" {
		return nil, errors.New(locale.T("服务返回的价格格式无效。"))
	}
	var entries []struct {
		ID       string `json:"id"`
		Billing  string `json:"billing_mode"`
		ListCost struct {
			Currency string `json:"currency"`
		} `json:"list_cost"`
		Tiers []struct {
			ID   string   `json:"id"`
			Cost *float64 `json:"cost"`
		} `json:"per_call_tiers"`
	}
	if json.Unmarshal(payload.Models, &entries) != nil {
		return nil, errors.New(locale.T("服务返回的价格格式无效。"))
	}
	result := []domain.ModelPricing{}
	for _, entry := range entries {
		if entry.ID == "" || len(utf16.Encode([]rune(entry.ID))) > 200 || entry.Billing != "per_image" || !currencyPattern.MatchString(entry.ListCost.Currency) {
			continue
		}
		prices := map[string]float64{}
		for _, tier := range entry.Tiers {
			if (tier.ID == "2k" || tier.ID == "4k") && tier.Cost != nil && *tier.Cost >= 0 {
				prices[tier.ID] = *tier.Cost
			}
		}
		if len(prices) > 0 {
			result = append(result, domain.ModelPricing{Model: entry.ID, Currency: entry.ListCost.Currency, Prices: prices})
		}
	}
	return result, nil
}
