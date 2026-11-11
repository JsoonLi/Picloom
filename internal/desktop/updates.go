package desktop

import "image2ai/internal/locale"
import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"time"

	"github.com/egoist/mygo"
	"image2ai/internal/domain"
)

const updateStateLimit = 64 << 10

var updatesEnabled = mygo.Updater.Enabled
var checkNativeUpdate = mygo.Updater.Check

type updateChoices struct {
	AutomaticChecks    *bool     `json:"automaticChecks"`
	AutomaticDownloads bool      `json:"automaticDownloads"`
	SkippedVersion     string    `json:"skippedVersion"`
	LastCheck          time.Time `json:"lastCheck"`
}

// readUpdateChoices checks the updater's settings before it reads or replaces
// them. The owning Store lock must already have been acquired by the caller.
func readUpdateChoices(directory string) (*updateChoices, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, name := range []string{"updater.json", "updater.json.tmp"} {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > updateStateLimit {
			return nil, errors.New(locale.T("更新设置不是有效的本机文件。"))
		}
	}
	file, err := root.Open("updater.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, updateStateLimit+1))
	if err != nil {
		return nil, err
	}
	var choices updateChoices
	var fields map[string]jsontext.Value
	if len(data) > updateStateLimit || json.Unmarshal(data, &fields) != nil || fields == nil || json.Unmarshal(data, &choices) != nil {
		return nil, errors.New(locale.T("更新设置文件损坏，请保留文件并修复后重试。"))
	}
	for _, key := range []string{"automaticChecks", "automaticDownloads"} {
		if raw, ok := fields[key]; ok && string(raw) == "null" {
			return nil, errors.New(locale.T("更新设置文件损坏，请保留文件并修复后重试。"))
		}
	}
	return &choices, nil
}

// ValidateUpdateSettings runs after the data-root ownership checks.
func (d *Desktop) ValidateUpdateSettings() error {
	_, err := readUpdateChoices(d.root)
	return err
}

func (d *Desktop) InitializeUpdates() error {
	d.updateMu.Lock()
	defer d.updateMu.Unlock()
	choices, err := readUpdateChoices(d.root)
	if err != nil {
		return err
	}
	if !updatesEnabled() {
		return nil
	}
	if choices != nil {
		return nil
	}
	// First launch only. A later user choice must never be reset on restart.
	on := true
	return writeUpdateFile(d.root, "updater.json", &updateChoices{AutomaticChecks: &on, AutomaticDownloads: true})
}

// UpdateStatus may touch the filesystem; the studio calls it in a worker.
func (d *Desktop) UpdateStatus() domain.UpdateStatus {
	d.updateMu.Lock()
	defer d.updateMu.Unlock()
	return d.updateStatusLocked()
}

func (d *Desktop) updateStatusLocked() domain.UpdateStatus {
	version := mygo.App.Version()
	if version == "" {
		version = locale.T("开发版")
	}
	status := domain.UpdateStatus{
		Version: version, Supported: updatesEnabled(),
		Ready: d.updateReady,
	}
	if choices, err := readUpdateChoices(d.root); err == nil {
		status.AutomaticChecks = choices == nil || choices.AutomaticChecks == nil || *choices.AutomaticChecks
		status.AutomaticDownloads = choices == nil || choices.AutomaticDownloads
		if choices != nil {
			status.LastCheck = choices.LastCheck
		}
	}
	if d.pendingUpdate != nil {
		status.AvailableVersion = d.pendingUpdate.Version
	}
	return status
}

func (d *Desktop) SetUpdatePreferences(checks, downloads bool) (domain.UpdateStatus, error) {
	d.updateMu.Lock()
	defer d.updateMu.Unlock()
	if !updatesEnabled() {
		return d.updateStatusLocked(), errors.New(locale.T("当前版本或安装位置不支持自动更新。"))
	}
	choices, err := readUpdateChoices(d.root)
	if err != nil {
		return d.updateStatusLocked(), err
	}
	if choices == nil {
		choices = &updateChoices{}
	}
	choices.AutomaticChecks, choices.AutomaticDownloads = &checks, downloads
	err = writeUpdateFile(d.root, "updater.json", choices)
	return d.updateStatusLocked(), err
}

// The studio/menu invokes this in a worker: the safety probes touch disk.
func (d *Desktop) CheckForUpdates() error {
	if !updatesEnabled() {
		return errors.New(locale.T("当前版本或安装位置不支持自动更新。"))
	}
	d.updateMu.Lock()
	_, err := readUpdateChoices(d.root)
	ready := d.updateReady
	d.updateMu.Unlock()
	if err != nil || ready {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	up, err := checkNativeUpdate(ctx)
	if err != nil {
		return err
	}
	d.updateMu.Lock()
	defer d.updateMu.Unlock()
	choices, err := readUpdateChoices(d.root)
	if err != nil {
		return err
	}
	if choices == nil {
		on := true
		choices = &updateChoices{AutomaticChecks: &on, AutomaticDownloads: true}
	}
	choices.LastCheck = time.Now()
	if err := writeUpdateFile(d.root, "updater.json", choices); err != nil {
		return err
	}
	d.pendingUpdate = up
	return nil
}

func (d *Desktop) InstallUpdate(ctx context.Context) error {
	d.updateMu.Lock()
	up, ready := d.pendingUpdate, d.updateReady
	d.updateMu.Unlock()
	if ready {
		return nil
	}
	if up == nil {
		return errors.New(locale.T("没有可安装的更新。"))
	}
	// MyGo verifies the signed archive before replacing application files.
	if err := up.Install(ctx, nil); err != nil {
		return err
	}
	d.updateMu.Lock()
	d.updateReady = true
	d.updateMu.Unlock()
	return nil
}

func (d *Desktop) Relaunch() { mygo.App.Relaunch() }

// writeUpdateFile keeps writes in the owned data root and refuses links.
func writeUpdateFile(directory, name string, value any) error {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range []string{name, name + ".tmp"} {
		if info, err := root.Lstat(path); err == nil {
			if !info.Mode().IsRegular() {
				return errors.New(locale.T("更新设置不是有效的本机文件。"))
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	limit := updateStateLimit
	if name == updateDraftFile {
		limit = updateDraftLimit
	}
	if len(data) > limit {
		return errors.New(locale.T("更新前的草稿过大，已暂停自动重启。"))
	}
	file, err := root.OpenFile(name+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(name+".tmp", name)
}
