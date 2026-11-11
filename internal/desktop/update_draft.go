package desktop

import (
	"encoding/json/v2"
	"errors"
	"io"
	"os"

	"image2ai/internal/domain"
	"image2ai/internal/locale"
)

const updateDraftFile = "update-draft.json"
const updateDraftLimit = 256 << 20

func (d *Desktop) SaveUpdateDraft(draft domain.UpdateDraft) error {
	return writeUpdateFile(d.root, updateDraftFile, draft)
}

func (d *Desktop) LoadUpdateDraft() (*domain.UpdateDraft, error) {
	root, err := os.OpenRoot(d.root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat(updateDraftFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > updateDraftLimit {
		return nil, errors.New(locale.T("更新前的草稿文件无效，已保留原文件。"))
	}
	file, err := root.Open(updateDraftFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var draft *domain.UpdateDraft
	if err := json.UnmarshalRead(io.LimitReader(file, updateDraftLimit+1), &draft); err != nil || draft == nil {
		return nil, errors.New(locale.T("更新前的草稿文件无效，已保留原文件。"))
	}
	return draft, nil
}

func (d *Desktop) ClearUpdateDraft() error {
	root, err := os.OpenRoot(d.root)
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(updateDraftFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
