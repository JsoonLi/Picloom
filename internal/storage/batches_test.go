package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
)

func TestDeleteBatchCanRetryAfterCleanupOrMetadataFailure(t *testing.T) {
	for _, failure := range []string{"files", "history", "batch"} {
		t.Run(failure, func(t *testing.T) {
			s := openTest(t)
			images := filepath.Join(s.Root(), "image-gen", "images")
			if err := os.MkdirAll(images, 0700); err != nil {
				t.Fatal(err)
			}
			var records []domain.ImageRecord
			for _, id := range []string{"first", "second", "single"} {
				path := filepath.Join(images, id+".png")
				if err := os.WriteFile(path, []byte(id), 0600); err != nil {
					t.Fatal(err)
				}
				batchID := "b1"
				if id == "single" {
					batchID = ""
				}
				records = append(records, domain.ImageRecord{ID: id, BatchID: batchID, ImagePath: path, MIMEType: "image/png"})
			}
			if err := s.ChangeHistory(func(rows *[]domain.ImageRecord) error { *rows = records; return nil }); err != nil {
				t.Fatal(err)
			}
			batch := domain.Batch{ID: "b1", SharedNames: []string{"style.png"}, SharedFiles: []string{"b1-0.png"}, Items: []domain.BatchItem{{ID: "i1", Status: "done", RecordID: "first"}, {ID: "i2", Status: "done", RecordID: "second"}}}
			if err := s.WriteBatchReference("b1-0.png", []byte("shared")); err != nil {
				t.Fatal(err)
			}
			if err := s.ChangeBatches(func(rows *[]domain.Batch) error { *rows = []domain.Batch{batch}; return nil }); err != nil {
				t.Fatal(err)
			}
			replace := s.replace
			if failure == "files" {
				// A changed source path must fail safely after the first file
				// was removed, without discarding the records needed to retry.
				if err := os.Remove(records[1].ImagePath); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(records[1].ImagePath, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(records[1].ImagePath, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				target := "history.json"
				if failure == "batch" {
					target = "batches.json"
				}
				s.replace = func(source, destination string) error {
					if filepath.Base(destination) == target {
						return errors.New("controlled metadata failure")
					}
					return replace(source, destination)
				}
			}
			if err := s.DeleteBatch("b1"); err == nil {
				t.Fatal("injected cleanup failure was ignored")
			}
			if batches, err := s.ReadBatches(); err != nil || len(batches) != 1 {
				t.Fatalf("failed deletion lost the retryable batch: %+v %v", batches, err)
			}
			wantHistory := 3
			if failure == "batch" {
				wantHistory = 1
			}
			if history, err := s.ReadHistory(); err != nil || len(history) != wantHistory {
				t.Fatalf("cleanup failure lost history needed for retry: %+v %v", history, err)
			}
			s.replace = replace
			if failure == "files" {
				if err := os.Remove(filepath.Join(records[1].ImagePath, "keep")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(records[1].ImagePath); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(records[1].ImagePath, []byte("second"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.DeleteBatch("b1"); err != nil {
				t.Fatalf("retry failed: %v", err)
			}
			if batches, err := s.ReadBatches(); err != nil || len(batches) != 0 {
				t.Fatalf("retry kept the batch: %+v %v", batches, err)
			}
			if history, err := s.ReadHistory(); err != nil || len(history) != 1 || history[0].ID != "single" {
				t.Fatalf("retry changed unrelated history: %+v %v", history, err)
			}
			for _, record := range records[:2] {
				if _, err := os.Stat(record.ImagePath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("retry left a batch image: %s %v", record.ID, err)
				}
			}
			if data, err := os.ReadFile(records[2].ImagePath); err != nil || string(data) != "single" {
				t.Fatalf("unrelated image changed: %q %v", data, err)
			}
			if _, err := s.ReadBatchReference("b1-0.png"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("retry left a shared reference: %v", err)
			}
		})
	}
}

func TestBatchesRoundTripAndDeleteRemovesOnlyTheirWorks(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if rows, err := s.ReadBatches(); err != nil || len(rows) != 0 {
		t.Fatalf("empty store: %v %v", rows, err)
	}
	if err = s.WriteBatchReference("b1-0.png", []byte("shared")); err != nil {
		t.Fatal(err)
	}
	if err = s.WriteBatchReference(`..\escape.png`, []byte("x")); err == nil {
		t.Fatal("path accepted as a batch reference name")
	}
	batch := domain.Batch{ID: "b1", CreatedAt: 1, SharedNames: []string{"style.png"}, SharedFiles: []string{"b1-0.png"}, Items: []domain.BatchItem{{ID: "i1", Status: "done", RecordID: "r1"}}}
	if err = s.ChangeBatches(func(rows *[]domain.Batch) error { *rows = append(*rows, batch); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = s.ChangeBatches(func(rows *[]domain.Batch) error {
		*rows = append(*rows, domain.Batch{ID: "bad", Items: []domain.BatchItem{{ID: "x", Status: "exploded"}}})
		return nil
	}); err == nil {
		t.Fatal("invalid batch saved")
	}
	image := filepath.Join(s.Root(), "image-gen", "images", "r1.png")
	_ = os.MkdirAll(filepath.Dir(image), 0700)
	_ = os.WriteFile(image, []byte("png"), 0600)
	if err = s.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		*rows = append(*rows,
			domain.ImageRecord{ID: "r1", BatchID: "b1", ImagePath: image, MIMEType: "image/png"},
			domain.ImageRecord{ID: "single", MIMEType: "image/png"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ReadBatches()
	if err != nil || len(rows) != 1 || rows[0].Items[0].RecordID != "r1" {
		t.Fatalf("read back %+v %v", rows, err)
	}
	if data, err := s.ReadBatchReference("b1-0.png"); err != nil || string(data) != "shared" {
		t.Fatalf("shared reference %q %v", data, err)
	}
	if err = s.DeleteBatch("b1"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ReadBatches(); len(rows) != 0 {
		t.Fatal("batch not removed")
	}
	if history, _ := s.ReadHistory(); len(history) != 1 || history[0].ID != "single" {
		t.Fatalf("history after delete: %+v", history)
	}
	if _, err := os.Stat(image); !os.IsNotExist(err) {
		t.Fatal("the batch's work file was kept")
	}
	if _, err := s.ReadBatchReference("b1-0.png"); err == nil {
		t.Fatal("the batch's shared reference was kept")
	}
}
