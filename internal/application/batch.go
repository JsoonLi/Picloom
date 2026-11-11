package application

import (
	"errors"
	"path/filepath"
	"slices"

	"image2ai/internal/domain"
	"image2ai/internal/locale"
)

var batchRatios = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}

// SaveBatchPreferences keeps the batch panel's choices for the next launch.
func (b *Backend) SaveBatchPreferences(p domain.BatchPreferences) (domain.Config, error) {
	if p.Concurrency < 0 || p.Concurrency > 16 ||
		!slices.Contains([]string{"", "source", "fixed"}, p.SizeMode) ||
		(p.Ratio != "" && !slices.Contains(batchRatios, p.Ratio)) ||
		(p.OutputDirectory != "" && !filepath.IsAbs(p.OutputDirectory)) {
		return domain.Config{}, errors.New(locale.T("批量设置无效。"))
	}
	return b.Store.UpdateConfig(func(c *domain.Config) error { c.Batch = p; return nil })
}

// PlanGeneration returns the request Generate would send for args.
func (b *Backend) PlanGeneration(args domain.GenerateArgs) (domain.RequestPlan, error) {
	return b.Generator.Plan(args)
}
