package storage

import (
	"os"
	"path/filepath"
	"testing"

	"image2ai/internal/domain"
)

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
