package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"image2ai/internal/generation"
	"image2ai/internal/locale"
)

var batchExtensions = []string{".png", ".jpg", ".jpeg", ".webp"}

// OpenBatchImages asks for any number of images to process.
func (d *Desktop) OpenBatchImages() ([]string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: d.parent(), Title: locale.T("添加待处理图"), Multiple: true, Filters: []mygo.FileFilter{{Name: locale.T("图片"), Extensions: []string{"png", "jpg", "jpeg", "webp"}}}})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return d.CollectImages(paths)
}

// CollectImages keeps the image files among paths and, for each folder, the
// images directly inside it sorted by name. Subfolders are not searched.
func (d *Desktop) CollectImages(paths []string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	add := func(path string) {
		if !slices.Contains(batchExtensions, strings.ToLower(filepath.Ext(path))) {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil || seen[abs] {
			return
		}
		seen[abs] = true
		result = append(result, abs)
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf(locale.T("无法读取：%s"), filepath.Base(path))
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				add(path)
			}
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf(locale.T("无法读取文件夹：%s"), filepath.Base(path))
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() {
				add(filepath.Join(path, entry.Name()))
			}
		}
	}
	return result, nil
}

// SaveBatchResult copies artwork id into dir as stem plus its real
// extension. A taken name gets -1, -2, …; no file is ever replaced.
func (d *Desktop) SaveBatchResult(id, dir, stem string) (string, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return "", errors.New(locale.T("输出目录无效。"))
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", errors.New(locale.T("输出目录不存在或不可用。"))
	}
	data, err := d.backend.ReadImage(id)
	if err != nil {
		return "", err
	}
	format, err := generation.ImageFormat(data)
	if err != nil {
		return "", errors.New(locale.T("图片无法解码。"))
	}
	stem = safeStem(stem)
	for n := 0; n < 10000; n++ {
		name := stem
		if n > 0 {
			name = fmt.Sprintf("%s-%d", stem, n)
		}
		path := filepath.Join(dir, name+"."+format)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", errors.New(locale.T("无法写入输出目录。"))
		}
		_, err = f.Write(data)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
			return "", errors.New(locale.T("无法写入输出目录。"))
		}
		return path, nil
	}
	return "", errors.New(locale.T("输出目录中同名文件过多。"))
}

// safeStem keeps a file name usable on every system.
func safeStem(stem string) string {
	stem = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(stem))
	stem = strings.TrimRight(stem, ". ")
	if stem == "" {
		return "result"
	}
	return stem
}
