// Package desktop implements explicit operating-system operations for the studio.
package desktop

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"image2ai/internal/locale"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	_ "golang.org/x/image/webp"
	"image2ai/internal/domain"
	"image2ai/internal/generation"
)

const maxReferenceBytes = 20 * 1024 * 1024

type Desktop struct {
	backend       domain.Backend
	root          string
	mu            sync.RWMutex
	window        *mygo.Window
	updateMu      sync.Mutex
	pendingUpdate *mygo.Update
	updateReady   bool
}

var _ domain.Desktop = (*Desktop)(nil)

func New(backend domain.Backend, root string) *Desktop { return &Desktop{backend: backend, root: root} }
func (d *Desktop) SetWindow(w *mygo.Window)            { d.mu.Lock(); d.window = w; d.mu.Unlock() }
func (d *Desktop) parent() *mygo.Window                { d.mu.RLock(); defer d.mu.RUnlock(); return d.window }
func (d *Desktop) DataDir() string                     { return d.root }
func (d *Desktop) ChooseDirectory(title, current string) (string, bool, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: d.parent(), Title: title, DefaultPath: current, Directory: true, CreateDirectories: true, ButtonLabel: locale.T("选择")})
	if err != nil || len(paths) == 0 {
		return "", false, err
	}
	return paths[0], true, nil
}
func (d *Desktop) SetLanguage(tag string) string {
	shown := Language(tag, mygo.App.PreferredLanguages())
	locale.Set(shown)
	if tag == "" || tag == "system" {
		mygo.App.SetLocale("")
	} else {
		mygo.App.SetLocale(shown)
	}
	if w := d.parent(); w != nil {
		w.SetTitle(locale.T("Picloom · 绘织"))
	}
	return shown
}

// Language returns the language to show for a setting: the chosen one, or
// for "system" the first of the computer's languages the studio speaks,
// else Simplified Chinese.
func Language(setting string, preferred []string) string {
	if setting == locale.Chinese || setting == locale.English {
		return setting
	}
	for _, tag := range preferred {
		lower := strings.ToLower(tag)
		switch {
		case lower == "en" || strings.HasPrefix(lower, "en-"):
			return locale.English
		case lower == "zh" || strings.HasPrefix(lower, "zh-"):
			return locale.Chinese
		}
	}
	return locale.Chinese
}

func (d *Desktop) SetTheme(source string) {
	switch source {
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}
func (d *Desktop) OpenImages(remaining int) ([]domain.Reference, error) {
	if remaining <= 0 {
		return nil, nil
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: d.parent(), Title: locale.T("添加参考图"), Multiple: true, Filters: []mygo.FileFilter{{Name: locale.T("图片"), Extensions: []string{"png", "jpg", "jpeg", "webp"}}}})
	if err != nil || len(paths) == 0 {
		return nil, err
	}
	return d.ImportFiles(paths, remaining)
}
func makeReference(name string, data []byte) (domain.Reference, error) {
	if len(data) == 0 || len(data) > maxReferenceBytes {
		return domain.Reference{}, errors.New(locale.T("每张参考图必须是有效图片，且不能超过 20 MB。"))
	}
	dimensions, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "webp") || dimensions.Width <= 0 || dimensions.Height <= 0 || int64(dimensions.Width)*int64(dimensions.Height) > 64*1024*1024 {
		return domain.Reference{}, errors.New(locale.T("参考图无法解码或图片尺寸过大，请选择 PNG、JPEG 或 WebP。"))
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return domain.Reference{}, errors.New(locale.T("参考图无法解码，请重新选择有效图片。"))
	}
	mime := "image/" + format
	return domain.Reference{ID: rand.Text(), Name: name, DataURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
}
func (d *Desktop) ImportFiles(paths []string, remaining int) ([]domain.Reference, error) {
	remaining = max(0, min(4, remaining))
	if len(paths) > remaining {
		paths = paths[:remaining]
	}
	refs := make([]domain.Reference, 0, len(paths))
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.New(locale.T("无法读取所选参考图。"))
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxReferenceBytes {
			f.Close()
			return nil, errors.New(locale.T("参考图必须是文件，且每张不能超过 20 MB。"))
		}
		data, err := io.ReadAll(io.LimitReader(f, maxReferenceBytes+1))
		f.Close()
		if err != nil {
			return nil, errors.New(locale.T("无法读取所选参考图。"))
		}
		ref, err := makeReference(filepath.Base(path), data)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
func (d *Desktop) PasteImages(remaining int) ([]domain.Reference, error) {
	if remaining <= 0 {
		return nil, nil
	}
	paths, err := mygo.Clipboard.ReadFiles()
	if err == nil && len(paths) > 0 {
		return d.ImportFiles(paths, remaining)
	}
	data := mygo.Clipboard.ReadImage()
	if len(data) == 0 {
		return nil, nil
	}
	ref, err := makeReference(locale.T("剪贴板图片.png"), data)
	if err != nil {
		return nil, err
	}
	return []domain.Reference{ref}, nil
}
func (d *Desktop) CopyText(value string) error { return mygo.Clipboard.Write(transfer.TextData(value)) }
func (d *Desktop) CopyImage(id string) error {
	data, err := d.backend.ReadImage(id)
	if err != nil {
		return err
	}
	var decoded image.Image
	format, err := generation.ImageFormat(data)
	if err != nil {
		return err
	}
	if format == "svg" {
		decoded, err = generation.Thumbnail(data, generation.MaxThumbnailSide, generation.MaxThumbnailSide)
	} else {
		decoded, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return errors.New(locale.T("图片无法解码。"))
	}
	var output bytes.Buffer
	if err = png.Encode(&output, decoded); err != nil {
		return errors.New(locale.T("图片复制失败。"))
	}
	return mygo.Clipboard.WriteImage(output.Bytes())
}
func (d *Desktop) ExportImage(id string) (bool, error) {
	data, err := d.backend.ReadImage(id)
	if err != nil {
		return false, err
	}
	format, err := generation.ImageFormat(data)
	if err != nil {
		return false, errors.New(locale.T("图片无法解码。"))
	}
	if format == "jpeg" {
		format = "jpg"
	}
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: d.parent(), Title: locale.T("导出原图"), DefaultPath: fmt.Sprintf("Picloom-%s.%s", id, format), Filters: []mygo.FileFilter{{Name: strings.ToUpper(format) + locale.T(" 图片"), Extensions: []string{format}}}})
	if err != nil || path == "" {
		return false, err
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		return false, errors.New(locale.T("导出失败，请检查目标文件夹是否可写。"))
	}
	return true, nil
}
