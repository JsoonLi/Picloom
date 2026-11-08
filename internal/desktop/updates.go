package desktop

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"image2ai/internal/domain"
)

const updateStateLimit = 64 << 10

type updateChoices struct {
	AutomaticChecks    *bool     `json:"automaticChecks"`
	AutomaticDownloads bool      `json:"automaticDownloads"`
	SkippedVersion     string    `json:"skippedVersion"`
	LastCheck          time.Time `json:"lastCheck"`
}

// readUpdateChoices checks the plugin's settings before it reads or replaces
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
			return nil, errors.New("更新设置不是有效的本机文件。")
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
		return nil, errors.New("更新设置文件损坏，请保留文件并修复后重试。")
	}
	for _, key := range []string{"automaticChecks", "automaticDownloads"} {
		if raw, ok := fields[key]; ok && string(raw) == "null" {
			return nil, errors.New("更新设置文件损坏，请保留文件并修复后重试。")
		}
	}
	return &choices, nil
}

// InitializeUpdates runs after the data-root ownership checks and before the
// updater's ready handler schedules its first background check.
func (d *Desktop) ValidateUpdateSettings() error {
	_, err := readUpdateChoices(d.root)
	return err
}

func (d *Desktop) InitializeUpdates() error {
	choices, err := readUpdateChoices(d.root)
	if err != nil {
		return err
	}
	if !mygo.Updater.Enabled() {
		return nil
	}
	if choices != nil {
		return nil
	}
	// First launch only. A later user choice must never be reset on restart.
	updater.SetAutomaticDownloads(true)
	choices, err = readUpdateChoices(d.root)
	if err != nil {
		return err
	}
	if choices == nil || !choices.AutomaticDownloads {
		return errors.New("无法保存自动更新设置，请检查数据目录权限。")
	}
	return nil
}

// UpdateStatus may touch the filesystem; the studio calls it in a worker.
func (d *Desktop) UpdateStatus() domain.UpdateStatus {
	version := mygo.App.Version()
	if version == "" {
		version = "开发版"
	}
	return domain.UpdateStatus{
		Version: version, Supported: mygo.Updater.Enabled(),
		AutomaticChecks:    updater.AutomaticChecks(),
		AutomaticDownloads: updater.AutomaticDownloads(),
		AvailableVersion:   updater.AvailableVersion(), LastCheck: updater.LastCheck(),
	}
}

func (d *Desktop) SetUpdatePreferences(checks, downloads bool) (domain.UpdateStatus, error) {
	if !mygo.Updater.Enabled() {
		return d.UpdateStatus(), errors.New("当前版本或安装位置不支持自动更新。")
	}
	if _, err := readUpdateChoices(d.root); err != nil {
		return d.UpdateStatus(), err
	}
	updater.SetAutomaticChecks(checks)
	updater.SetAutomaticDownloads(downloads)
	saved, err := readUpdateChoices(d.root)
	if err == nil && (saved == nil || saved.AutomaticChecks == nil || *saved.AutomaticChecks != checks || saved.AutomaticDownloads != downloads) {
		err = errors.New("无法保存更新设置，请检查数据目录权限。")
	}
	return d.UpdateStatus(), err
}

// The studio/menu invokes this in a worker: the safety probes touch disk.
func (d *Desktop) CheckForUpdates() error {
	if !mygo.Updater.Enabled() {
		return errors.New("当前版本或安装位置不支持自动更新。")
	}
	if _, err := readUpdateChoices(d.root); err != nil {
		return err
	}
	updater.CheckForUpdates()
	return nil
}
