// Package application composes the UI's narrow capabilities without a generic IPC bridge.
package application

import (
	"context"
	"errors"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
	"image2ai/internal/network"
	"image2ai/internal/storage"
	"unicode/utf16"
)

type Backend struct {
	Store     *storage.Store
	Generator *generation.Service
}

var _ domain.Backend = (*Backend)(nil)

func New(store *storage.Store) *Backend {
	return &Backend{Store: store, Generator: generation.New(store)}
}
func (b *Backend) LoadConfig() (domain.Config, error) { return b.Store.ReadConfig() }
func (b *Backend) SaveProviders(values []domain.Provider) (domain.Config, error) {
	providers, err := generation.NormalizeProviders(values)
	if err != nil {
		return domain.Config{}, err
	}
	return b.Store.UpdateConfig(func(c *domain.Config) error { c.Providers = providers; return nil })
}
func (b *Backend) SaveProxy(proxy domain.ProxyConfig) (domain.Config, error) {
	if err := network.ValidateProxy(proxy); err != nil {
		return domain.Config{}, err
	}
	return b.Store.UpdateConfig(func(c *domain.Config) error { c.Proxy = proxy; return nil })
}
func (b *Backend) SaveTheme(theme string) (domain.Config, error) {
	if theme != "system" && theme != "light" && theme != "dark" {
		return domain.Config{}, errors.New("外观设置无效。")
	}
	return b.Store.UpdateConfig(func(c *domain.Config) error { c.Theme = theme; return nil })
}
func (b *Backend) SavePreferences(p domain.GenerationPreferences) (domain.Config, error) {
	if err := generation.ValidateGIFSettings(p.GIFSize, p.GIFFrameCount, p.GIFFrameDurationMS); err != nil {
		return domain.Config{}, err
	}
	valid := false
	for _, r := range []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"} {
		if r == p.Ratio {
			valid = true
		}
	}
	if !valid || !generation.ValidOutputFormat(p.OutputFormat) || (p.Resolution != "2k" && p.Resolution != "4k") || len(utf16.Encode([]rune(p.ProviderID))) > 200 || len(utf16.Encode([]rune(p.Model))) > 200 {
		return domain.Config{}, errors.New("生成偏好无效。")
	}
	return b.Store.UpdateConfig(func(c *domain.Config) error { c.GenerationPreferences = p; return nil })
}
func (b *Backend) DiscoverModels(ctx context.Context, p domain.Provider) ([]string, error) {
	return b.Generator.DiscoverModels(ctx, p)
}
func (b *Backend) DiscoverPricing(ctx context.Context, id string) ([]domain.ModelPricing, error) {
	return b.Generator.DiscoverPricing(ctx, id)
}
func (b *Backend) Generate(ctx context.Context, args domain.GenerateArgs, progress func(domain.Progress)) (domain.ImageRecord, error) {
	return b.Generator.Generate(ctx, args, progress)
}
func (b *Backend) ListHistory(q domain.HistoryQuery) (domain.HistoryPage, error) {
	return b.Store.List(q)
}
func (b *Backend) Favorite(id string, value bool) (domain.ImageRecord, error) {
	return b.Store.Favorite(id, value)
}
func (b *Backend) Delete(id string) error { return b.Store.Delete(id) }
func (b *Backend) ReadImage(id string) ([]byte, error) {
	return validatedImageRead(b.Store.ReadImage(id))
}
func (b *Backend) ReadThumbnail(id string, w, h int) ([]byte, error) {
	if w < 1 || h < 1 || w > generation.MaxThumbnailSide || h > generation.MaxThumbnailSide {
		return nil, errors.New("缩略图尺寸无效。")
	}
	if data, err := b.Store.ReadThumbnail(id, w, h); err == nil && generation.ValidateImageConfig(data) == nil {
		return data, nil
	}
	original, err := b.ReadImage(id)
	if err != nil {
		return nil, err
	}
	img, err := generation.Thumbnail(original, w, h)
	if err != nil {
		return nil, err
	}
	format, _ := generation.ImageFormat(original)
	if bounds, err := generation.ImageSize(original); err == nil && img.Bounds().Size() == bounds && format != "svg" && format != "gif" {
		return original, nil // already small: no copy, no re-encoding loss
	}
	data, err := generation.EncodeThumbnail(img)
	if err != nil {
		return nil, err
	}
	// The cache only saves later decodes; a failed write still shows the image.
	_ = b.Store.WriteThumbnail(id, w, h, data)
	return data, nil
}
func (b *Backend) ReadReference(id string, index int) ([]byte, error) {
	return validatedImageRead(b.Store.ReadReference(id, index))
}

// Legacy history may predate the generation validator. Bound every image at
// the shared read boundary before thumbnail, preview or clipboard decoding.
func validatedImageRead(data []byte, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	if err := generation.ValidateImageConfig(data); err != nil {
		return nil, err
	}
	return data, nil
}
