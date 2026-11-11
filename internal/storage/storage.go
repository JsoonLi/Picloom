// Package storage reads the Electron data format without changing its schema.
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image2ai/internal/locale"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"image2ai/internal/domain"
)

const maxConfigBytes = 2 * 1024 * 1024
const maxHistoryBytes = 64 * 1024 * 1024
const maxImageBytes = 40 * 1024 * 1024
const maxThumbnailBytes = 4 * 1024 * 1024

var ErrClosed error = locale.Error("数据存储已关闭。")
var errOutside error = locale.Error("禁止访问此文件或外部链接。")
var retryDelays = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}

type Store struct {
	root string
	// originalRoot preserves the explicit, trusted data-root alias used by legacy paths.
	originalRoot string
	mu           sync.Mutex
	lock         *os.File
	closed       bool
	replace      func(string, string) error
	// history caches the parsed history file while its size and
	// modification time stay the same; writes through ChangeHistory drop it.
	history      []domain.ImageRecord
	historyStamp fileStamp
}

type fileStamp struct {
	size    int64
	modTime time.Time
}

var _ domain.Store = (*Store)(nil)

func Open(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New(locale.T("数据目录不能为空。"))
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	lock, err := acquireLock(filepath.Join(canonical, ".image2ai-native.lock"))
	if err != nil {
		return nil, fmt.Errorf(locale.T("Picloom 数据目录正被其他进程使用，或无法锁定：%w"), err)
	}
	s := &Store{root: canonical, originalRoot: absolute, lock: lock, replace: replaceFile}
	if _, err = s.readConfig(); err == nil {
		_, err = s.readHistory()
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Root() string { return s.root }
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.lock.Close()
}
func (s *Store) ReadConfig() (domain.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return domain.Config{}, ErrClosed
	}
	return s.readConfig()
}
func (s *Store) ReadHistory() ([]domain.ImageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	rows, err := s.readHistory()
	return slices.Clone(rows), err
}
func (s *Store) UpdateConfig(change func(*domain.Config) error) (domain.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return domain.Config{}, ErrClosed
	}
	c, err := s.readConfig()
	if err != nil {
		return domain.Config{}, err
	}
	if change == nil {
		return domain.Config{}, errors.New(locale.T("配置更新无效。"))
	}
	if err = change(&c); err != nil {
		return domain.Config{}, err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return domain.Config{}, err
	}
	if err = validateConfig(data); err != nil {
		return domain.Config{}, fmt.Errorf(locale.T("配置更新无效：%w"), err)
	}
	if err = s.write("config.json", data, maxConfigBytes); err != nil {
		return domain.Config{}, err
	}
	// Return a detached snapshot, even if the caller retained its callback input.
	var result domain.Config
	err = json.Unmarshal(data, &result)
	return result, err
}
func (s *Store) ChangeHistory(change func(*[]domain.ImageRecord) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	cached, err := s.readHistory()
	if err != nil {
		return err
	}
	if change == nil {
		return errors.New(locale.T("历史更新无效。"))
	}
	rows := slices.Clone(cached)
	if err = change(&rows); err != nil {
		return err
	}
	if rows == nil {
		rows = []domain.ImageRecord{}
	}
	for i := range rows {
		if rows[i].ReferencePaths == nil {
			rows[i].ReferencePaths = []string{}
		}
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	if err = validateHistory(data); err != nil {
		return fmt.Errorf(locale.T("历史更新无效：%w"), err)
	}
	s.history = nil
	return s.write(filepath.Join("image-gen", "history.json"), data, maxHistoryBytes)
}

func defaults() domain.Config {
	return domain.Config{
		Providers: []domain.Provider{{ID: "openai-compatible", Name: locale.T("OpenAI 兼容"), BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-image-1"}, Resolutions: []string{"2k", "4k"}, GenerationsPath: "/images/generations", EditsPath: "/images/edits"}},
		Proxy:     domain.ProxyConfig{Mode: "system", Bypass: "<local>,localhost,127.0.0.1"}, Theme: "system",
		GenerationPreferences: domain.GenerationPreferences{Ratio: "3:4", Resolution: "2k"},
	}
}
func (s *Store) readConfig() (domain.Config, error) {
	raw, err := s.readMetadata("config.json", maxConfigBytes)
	if errors.Is(err, os.ErrNotExist) {
		return defaults(), nil
	}
	if err != nil {
		return domain.Config{}, err
	}
	if err = validateConfig(raw); err != nil {
		return domain.Config{}, fmt.Errorf(locale.T("配置文件损坏，请备份后修复：%s"), filepath.Join(s.root, "config.json"))
	}
	c := defaults()
	if err = json.Unmarshal(raw, &c); err != nil {
		return domain.Config{}, err
	}
	for i := range c.Providers {
		if c.Providers[i].Resolutions == nil {
			c.Providers[i].Resolutions = []string{"2k", "4k"}
		}
		if c.Providers[i].DefaultResolution == "" {
			c.Providers[i].DefaultResolution = c.Providers[i].Resolutions[0]
		}
	}
	return c, nil
}

// readHistory returns the parsed history, shared with the cache: callers
// that change or return the rows must copy them first.
func (s *Store) readHistory() ([]domain.ImageRecord, error) {
	relative := filepath.Join("image-gen", "history.json")
	r, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(relative)
	if errors.Is(err, os.ErrNotExist) {
		s.history = nil
		return []domain.ImageRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	stamp := fileStamp{size: info.Size(), modTime: info.ModTime()}
	if s.history != nil && s.historyStamp == stamp {
		return s.history, nil
	}
	raw, err := readBounded(f, maxHistoryBytes)
	if err != nil {
		return nil, err
	}
	if err = validateHistory(raw); err != nil {
		return nil, fmt.Errorf(locale.T("历史记录文件损坏，请备份后修复：%s"), filepath.Join(s.root, relative))
	}
	var rows []domain.ImageRecord
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.ImageRecord{}
	}
	s.history, s.historyStamp = rows, stamp
	return rows, nil
}

func (s *Store) readMetadata(relative string, limit int64) ([]byte, error) {
	r, err := os.OpenRoot(s.root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(relative)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f, limit)
}
func readBounded(f *os.File, limit int64) ([]byte, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(locale.T("只能读取普通文件。"))
	}
	if info.Size() > limit {
		return nil, errors.New(locale.T("文件超过允许的大小。"))
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New(locale.T("文件超过允许的大小。"))
	}
	return raw, nil
}
func (s *Store) write(relative string, data []byte, limit int) error {
	if len(data) > limit {
		return errors.New(locale.T("写入内容超过允许的大小。"))
	}
	destination := filepath.Join(s.root, relative)
	directory := filepath.Dir(destination)
	// OpenRoot prevents a pre-existing image-gen junction from escaping root.
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	if parent := filepath.Dir(relative); parent != "." {
		if err = root.MkdirAll(parent, 0700); err != nil {
			return err
		}
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	if canonical != s.root && !within(canonical, s.root) {
		return errOutside
	}
	if info, err := os.Lstat(destination); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errOutside
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(canonical, ".image2ai-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	target := filepath.Join(canonical, filepath.Base(destination))
	for attempt := 0; ; attempt++ {
		err = s.replace(name, target)
		if err == nil {
			break
		}
		if !transientReplace(err) || attempt == len(retryDelays) {
			return err
		}
		time.Sleep(retryDelays[attempt])
	}
	// The rename is the transaction's commit point. A directory-sync error
	// afterwards must not tell callers to roll back already-committed images.
	_ = syncDirectory(canonical)
	return nil
}

func within(file, dir string) bool {
	relative, err := filepath.Rel(dir, file)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func (s *Store) List(q domain.HistoryQuery) (domain.HistoryPage, error) {
	rows, err := s.ReadHistory()
	if err != nil {
		return domain.HistoryPage{}, err
	}
	query := strings.ToLower(strings.TrimSpace(q.Search))
	favoriteCount := 0
	filtered := make([]domain.ImageRecord, 0, len(rows))
	for _, row := range rows {
		if row.Favorite {
			favoriteCount++
		}
		if q.FavoritesOnly && !row.Favorite {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(row.Prompt+" "+row.Model+" "+row.ProviderName), query) {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].CreatedAt > filtered[j].CreatedAt })
	if q.All {
		return domain.HistoryPage{Items: filtered, Total: len(filtered), Page: 1, PageSize: max(1, len(filtered)), TotalPages: 1, FavoriteCount: favoriteCount}, nil
	}
	size := q.PageSize
	if size <= 0 {
		size = 12
	}
	if size > 48 {
		size = 48
	}
	totalPages := (len(filtered) + size - 1) / size
	if totalPages < 1 {
		totalPages = 1
	}
	page := q.Page
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * size
	end := start + size
	if end > len(filtered) {
		end = len(filtered)
	}
	return domain.HistoryPage{Items: filtered[start:end], Total: len(filtered), Page: page, PageSize: size, TotalPages: totalPages, FavoriteCount: favoriteCount}, nil
}
func (s *Store) Favorite(id string, value bool) (domain.ImageRecord, error) {
	var result domain.ImageRecord
	err := s.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		for i := range *rows {
			if (*rows)[i].ID == id {
				(*rows)[i].Favorite = value
				result = (*rows)[i]
				result.ReferencePaths = append([]string{}, result.ReferencePaths...)
				return nil
			}
		}
		return errors.New(locale.T("历史记录不存在。"))
	})
	return result, err
}
func (s *Store) find(id string) (domain.ImageRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return domain.ImageRecord{}, ErrClosed
	}
	rows, err := s.readHistory()
	if err != nil {
		return domain.ImageRecord{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return domain.ImageRecord{}, errors.New(locale.T("历史记录不存在。"))
}
func (s *Store) ReadImage(id string) ([]byte, error) {
	row, err := s.find(id)
	if err != nil {
		return nil, err
	}
	return s.readOwned(row.ImagePath, "images")
}
func (s *Store) ReadReference(id string, index int) ([]byte, error) {
	if index < 0 || index > 3 {
		return nil, errors.New(locale.T("参考图序号无效。"))
	}
	row, err := s.find(id)
	if err != nil {
		return nil, err
	}
	if index >= len(row.ReferencePaths) {
		return nil, errors.New(locale.T("参考图不存在。"))
	}
	return s.readOwned(row.ReferencePaths[index], "references")
}

// thumbnailPath names an image's cached thumbnail of one size. The ID is
// hashed so history contents never become path segments.
func thumbnailPath(id string, w, h int) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join("image-gen", "thumbnails", fmt.Sprintf("%dx%d", w, h), hex.EncodeToString(sum[:16])+".thumb")
}

// ReadThumbnail returns a cached thumbnail, or os.ErrNotExist.
func (s *Store) ReadThumbnail(id string, w, h int) ([]byte, error) {
	if s.isClosed() {
		return nil, ErrClosed
	}
	return s.readMetadata(thumbnailPath(id, w, h), maxThumbnailBytes)
}

// WriteThumbnail caches a thumbnail of a record that still exists.
func (s *Store) WriteThumbnail(id string, w, h int, data []byte) error {
	if _, err := s.find(id); err != nil {
		return err
	}
	return s.write(thumbnailPath(id, w, h), data, maxThumbnailBytes)
}

func (s *Store) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// removeThumbnails deletes every cached size of an image's thumbnail.
func (s *Store) removeThumbnails(id string) error {
	root, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer root.Close()
	sizes, err := fs.ReadDir(root.FS(), "image-gen/thumbnails")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, size := range sizes {
		var w, h int
		if !size.IsDir() {
			continue
		}
		if _, err := fmt.Sscanf(size.Name(), "%dx%d", &w, &h); err != nil {
			continue
		}
		if err := root.Remove(thumbnailPath(id, w, h)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ownedRoot opens the folder holding an image the app saved: under the data
// directory, or under a folder chosen for works now or before.
func (s *Store) ownedRoot(file, bucket string) (*os.Root, string, error) {
	root, relative, err := s.ownedIn(file, filepath.Join(s.root, "image-gen", bucket), s.root, true)
	if !errors.Is(err, errOutside) {
		return root, relative, err
	}
	config, e := s.ReadConfig()
	if e != nil {
		return nil, "", errOutside
	}
	for _, base := range append([]string{config.ImageDirectory}, config.PreviousImageDirectories...) {
		if base == "" || !filepath.IsAbs(base) {
			continue
		}
		// Generation stores canonical paths; older settings may name an alias.
		if canonical, e := filepath.EvalSymlinks(base); e == nil && canonical != base {
			root, relative, err = s.ownedIn(file, filepath.Join(canonical, bucket), canonical, false)
			if !errors.Is(err, errOutside) {
				return root, relative, err
			}
		}
		root, relative, err = s.ownedIn(file, filepath.Join(base, bucket), base, false)
		if !errors.Is(err, errOutside) {
			return root, relative, err
		}
	}
	return nil, "", errOutside
}

// ownedIn opens directory when file lies inside it and no link leads out of
// container. alias maps legacy paths through the data directory's alias.
func (s *Store) ownedIn(file, directory, container string, alias bool) (*os.Root, string, error) {
	absolute, err := filepath.Abs(file)
	if err == nil && alias && s.originalRoot != s.root && within(absolute, s.originalRoot) {
		relative, e := filepath.Rel(s.originalRoot, absolute)
		if e != nil {
			return nil, "", errOutside
		}
		absolute = filepath.Join(s.root, relative)
	}
	if err != nil || !within(absolute, directory) {
		return nil, "", errOutside
	}
	if !alias {
		if container, err = filepath.EvalSymlinks(container); err != nil {
			return nil, "", err
		}
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, "", err
	}
	if !within(canonical, container) {
		return nil, "", errOutside
	}
	relative, err := filepath.Rel(directory, absolute)
	if err != nil {
		return nil, "", errOutside
	}
	// EvalSymlinks checks the leaf too, so delete never follows an external link.
	target, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, "", err
	}
	if !within(target, canonical) {
		return nil, "", errOutside
	}
	root, err := os.OpenRoot(canonical)
	return root, relative, err
}
func (s *Store) readOwned(file, bucket string) ([]byte, error) {
	root, relative, err := s.ownedRoot(file, bucket)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f, maxImageBytes)
}
func (s *Store) Delete(id string) error {
	var removed domain.ImageRecord
	err := s.ChangeHistory(func(rows *[]domain.ImageRecord) error {
		for i, row := range *rows {
			if row.ID == id {
				removed = row
				*rows = append((*rows)[:i], (*rows)[i+1:]...)
				return nil
			}
		}
		return errors.New(locale.T("历史记录不存在。"))
	})
	if err != nil {
		return err
	}
	// A thumbnail left behind only costs disk space: it never shows again.
	_ = s.removeThumbnails(id)
	files := []struct{ path, bucket string }{{removed.ImagePath, "images"}}
	for _, file := range removed.ReferencePaths {
		files = append(files, struct{ path, bucket string }{file, "references"})
	}
	for _, file := range files {
		root, relative, err := s.ownedRoot(file.path, file.bucket)
		if errors.Is(err, errOutside) || errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		err = root.Remove(relative)
		root.Close()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// BackupLegacy snapshots the original bytes once, before native mutations.
func (s *Store) BackupLegacy() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	destination := filepath.Join(s.root, "native-backup-v1")
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New(locale.T("原生迁移备份路径无效。"))
		}
		raw, err := s.readMetadata(filepath.Join("native-backup-v1", "complete.json"), 1024)
		if err != nil {
			return fmt.Errorf(locale.T("迁移备份不完整，请保留现有备份后重试：%w"), err)
		}
		var marker struct {
			Version int `json:"version"`
		}
		if json.Unmarshal(raw, &marker) != nil || marker.Version != 1 {
			return errors.New(locale.T("迁移备份标记无效。"))
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Validate first: a backup must never make corrupt source data writable.
	if _, err := s.readConfig(); err != nil {
		return err
	}
	if _, err := s.readHistory(); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(s.root, ".native-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	for _, item := range []struct {
		source, target string
		limit          int64
	}{{"config.json", "config.json", maxConfigBytes}, {filepath.Join("image-gen", "history.json"), "history.json", maxHistoryBytes}} {
		raw, err := s.readMetadata(item.source, item.limit)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		f, err := os.OpenFile(filepath.Join(temporary, item.target), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(raw)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	marker, err := os.OpenFile(filepath.Join(temporary, "complete.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := marker.Write([]byte(`{"version":1}`))
	syncErr := marker.Sync()
	closeErr := marker.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = syncDirectory(temporary); err != nil {
		return err
	}
	if err = os.Rename(temporary, destination); err != nil {
		return err
	}
	return syncDirectory(s.root)
}
