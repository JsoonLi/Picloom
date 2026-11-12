package application

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"
	"unicode/utf16"

	"image2ai/internal/domain"
	"image2ai/internal/generation"
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

func (b *Backend) ListBatches() ([]domain.Batch, error) { return b.Store.ReadBatches() }

// CreateBatch gives the batch its ID and time, stores its shared
// references and saves it.
func (b *Backend) CreateBatch(batch domain.Batch, shared [][]byte) (domain.Batch, error) {
	if len(shared) > 3 || len(shared) != len(batch.SharedNames) || len(batch.Items) == 0 || len(utf16.Encode([]rune(batch.Prompt))) > 20000 {
		return domain.Batch{}, errors.New(locale.T("批次无效。"))
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return domain.Batch{}, err
	}
	batch.ID = hex.EncodeToString(id[:])
	batch.CreatedAt = time.Now().UnixMilli()
	batch.SharedFiles = []string{}
	for i, data := range shared {
		format, err := generation.ImageFormat(data)
		if err != nil {
			return domain.Batch{}, errors.New(locale.T("共用参考图无效。"))
		}
		name := fmt.Sprintf("%s-%d.%s", batch.ID, i, format)
		if err = b.Store.WriteBatchReference(name, data); err != nil {
			return domain.Batch{}, err
		}
		batch.SharedFiles = append(batch.SharedFiles, name)
	}
	err := b.Store.ChangeBatches(func(rows *[]domain.Batch) error {
		*rows = append(*rows, batch)
		return nil
	})
	return batch, err
}

// UpdateBatchItems replaces the batch's items that have the same IDs.
func (b *Backend) UpdateBatchItems(id string, items []domain.BatchItem) error {
	return b.Store.ChangeBatches(func(rows *[]domain.Batch) error {
		for i := range *rows {
			if (*rows)[i].ID != id {
				continue
			}
			for _, item := range items {
				for j := range (*rows)[i].Items {
					if (*rows)[i].Items[j].ID == item.ID {
						(*rows)[i].Items[j] = item
					}
				}
			}
			return nil
		}
		return errors.New(locale.T("批次不存在。"))
	})
}

func (b *Backend) ReadBatchReference(id string, index int) ([]byte, error) {
	batches, err := b.Store.ReadBatches()
	if err != nil {
		return nil, err
	}
	for _, batch := range batches {
		if batch.ID == id {
			if index < 0 || index >= len(batch.SharedFiles) {
				return nil, errors.New(locale.T("参考图不存在。"))
			}
			return validatedImageRead(b.Store.ReadBatchReference(batch.SharedFiles[index]))
		}
	}
	return nil, errors.New(locale.T("批次不存在。"))
}

func (b *Backend) DeleteBatch(id string) error { return b.Store.DeleteBatch(id) }
