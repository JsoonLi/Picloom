package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"image2ai/internal/domain"
	"image2ai/internal/locale"
)

const maxBatchesBytes = 32 * 1024 * 1024

// batchReferenceDir holds shared batch references inside the data directory.
var batchReferenceDir = filepath.Join("image-gen", "batch-references")

var batchStatuses = []string{"queued", "done", "error", "canceled"}

func validateBatches(raw []byte) error {
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return errors.New(locale.T("批次记录无效"))
	}
	ids := map[string]bool{}
	for _, raw := range rows {
		var batch domain.Batch
		if json.Unmarshal(raw, &batch) != nil || batch.ID == "" || ids[batch.ID] || len(batch.ID) > 64 || batch.Items == nil {
			return errors.New(locale.T("批次记录无效"))
		}
		ids[batch.ID] = true
		if len(batch.SharedFiles) != len(batch.SharedNames) || len(batch.SharedFiles) > 3 {
			return errors.New(locale.T("批次记录无效"))
		}
		for _, name := range batch.SharedFiles {
			if !validBatchFile(name) {
				return errors.New(locale.T("批次记录无效"))
			}
		}
		items := map[string]bool{}
		for _, item := range batch.Items {
			if item.ID == "" || items[item.ID] || len(item.ID) > 64 || !slices.Contains(batchStatuses, item.Status) {
				return errors.New(locale.T("批次记录无效"))
			}
			items[item.ID] = true
		}
	}
	return nil
}

// validBatchFile accepts a plain file name, never a path.
func validBatchFile(name string) bool {
	return name != "" && name == filepath.Base(name) && !strings.ContainsAny(name, `/\:`) && name != "." && name != ".."
}

func (s *Store) readBatches() ([]domain.Batch, error) {
	raw, err := s.readMetadata(filepath.Join("image-gen", "batches.json"), maxBatchesBytes)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Batch{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err = validateBatches(raw); err != nil {
		return nil, fmt.Errorf(locale.T("批次记录文件损坏，请备份后修复：%s"), filepath.Join(s.root, "image-gen", "batches.json"))
	}
	var rows []domain.Batch
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ReadBatches returns every batch, newest first.
func (s *Store) ReadBatches() ([]domain.Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	rows, err := s.readBatches()
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].CreatedAt > rows[j].CreatedAt })
	return rows, err
}

// ChangeBatches applies change to the batches and saves them, all or nothing.
func (s *Store) ChangeBatches(change func(*[]domain.Batch) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return s.changeBatches(change)
}

func (s *Store) changeBatches(change func(*[]domain.Batch) error) error {
	rows, err := s.readBatches()
	if err != nil {
		return err
	}
	if err = change(&rows); err != nil {
		return err
	}
	if rows == nil {
		rows = []domain.Batch{}
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err = validateBatches(data); err != nil {
		return fmt.Errorf(locale.T("批次更新无效：%w"), err)
	}
	return s.write(filepath.Join("image-gen", "batches.json"), data, maxBatchesBytes)
}

// WriteBatchReference stores a shared reference under a plain file name.
func (s *Store) WriteBatchReference(name string, data []byte) error {
	if !validBatchFile(name) {
		return errOutside
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return s.write(filepath.Join(batchReferenceDir, name), data, maxImageBytes)
}

// ReadBatchReference returns a stored shared reference.
func (s *Store) ReadBatchReference(name string) ([]byte, error) {
	if !validBatchFile(name) {
		return nil, errOutside
	}
	if s.isClosed() {
		return nil, ErrClosed
	}
	return s.readMetadata(filepath.Join(batchReferenceDir, name), maxImageBytes)
}

func (s *Store) removeBatchReference(name string) error {
	if !validBatchFile(name) {
		return errOutside
	}
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(filepath.Join(batchReferenceDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// DeleteBatch removes batch id, its shared references and every work made
// by it. Copies in the batch's output folder are not touched.
func (s *Store) DeleteBatch(id string) error {
	batches, err := s.ReadBatches()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(batches, func(batch domain.Batch) bool { return batch.ID == id })
	if index < 0 {
		return errors.New(locale.T("批次不存在。"))
	}
	batch := batches[index]
	history, err := s.ReadHistory()
	if err != nil {
		return err
	}
	// Keep both records until file cleanup succeeds. Missing files are safe
	// to retry, whereas losing their history paths would strand the rest.
	for _, record := range history {
		if record.BatchID == id {
			if err = s.removeRecordFiles(record); err != nil {
				return err
			}
		}
	}
	for _, name := range batch.SharedFiles {
		if err = s.removeBatchReference(name); err != nil {
			return err
		}
	}
	if err = s.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		*rows = slices.DeleteFunc(*rows, func(record domain.ImageRecord) bool { return record.BatchID == id })
		return nil
	}); err != nil {
		return err
	}
	// The batch is the retry handle, so remove it only after all cleanup
	// and the history update have completed.
	return s.ChangeBatches(func(rows *[]domain.Batch) error {
		*rows = slices.DeleteFunc(*rows, func(batch domain.Batch) bool { return batch.ID == id })
		return nil
	})
}
