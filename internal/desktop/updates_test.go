package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestUpdatePreferencesFirstLaunchAndSavedChoices(t *testing.T) {
	root := t.TempDir()
	choices, err := readUpdateChoices(root)
	if err != nil || choices != nil {
		t.Fatalf("first launch: %v", err)
	}
	data := []byte("{\"automaticChecks\":false,\"automaticDownloads\":false}")
	if err := os.WriteFile(filepath.Join(root, "updater.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	choices, err = readUpdateChoices(root)
	if err != nil || choices == nil || choices.AutomaticChecks == nil || *choices.AutomaticChecks || choices.AutomaticDownloads {
		t.Fatalf("saved choices lost: %v", err)
	}
	saved, _ := os.ReadFile(filepath.Join(root, "updater.json"))
	if string(saved) != string(data) {
		t.Fatal("reading must not reset user preferences")
	}
}

func enableTestUpdates(t *testing.T) {
	t.Helper()
	oldEnabled, oldCheck := updatesEnabled, checkNativeUpdate
	updatesEnabled = func() bool { return true }
	t.Cleanup(func() { updatesEnabled, checkNativeUpdate = oldEnabled, oldCheck })
}

func TestUpdateInitializationKeepsSavedOptOut(t *testing.T) {
	enableTestUpdates(t)
	d := &Desktop{root: t.TempDir()}
	if err := d.InitializeUpdates(); err != nil {
		t.Fatal(err)
	}
	if status := d.UpdateStatus(); !status.AutomaticChecks || !status.AutomaticDownloads {
		t.Fatal("first launch must enable automatic updates")
	}
	if _, err := d.SetUpdatePreferences(false, false); err != nil {
		t.Fatal(err)
	}
	if err := d.InitializeUpdates(); err != nil {
		t.Fatal(err)
	}
	if status := d.UpdateStatus(); status.AutomaticChecks || status.AutomaticDownloads {
		t.Fatal("restart reset saved update preferences")
	}
}

func TestNativeUpdateCheckKeepsPreferencesChangedDuringNetworkRequest(t *testing.T) {
	enableTestUpdates(t)
	d := &Desktop{root: t.TempDir()}
	if err := d.InitializeUpdates(); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	checkNativeUpdate = func(ctx context.Context) (*mygo.Update, error) {
		if _, ok := ctx.Deadline(); !ok {
			return nil, errors.New("missing timeout")
		}
		close(started)
		<-release
		return &mygo.Update{Version: "0.9.0"}, nil
	}
	done := make(chan error, 1)
	go func() { done <- d.CheckForUpdates() }()
	<-started
	if _, err := d.SetUpdatePreferences(false, false); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	status := d.UpdateStatus()
	if status.AutomaticChecks || status.AutomaticDownloads || status.AvailableVersion != "0.9.0" || time.Since(status.LastCheck) > time.Second {
		t.Fatalf("check overwrote newer preferences or failed to publish its result: %+v", status)
	}
	checkNativeUpdate = func(context.Context) (*mygo.Update, error) { return nil, errors.New("offline") }
	if err := d.CheckForUpdates(); err == nil {
		t.Fatal("failed check reported success")
	}
	if !d.UpdateStatus().LastCheck.Equal(status.LastCheck) {
		t.Fatal("failed check changed last successful check")
	}
	checkNativeUpdate = func(context.Context) (*mygo.Update, error) { return nil, nil }
	if err := d.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	if d.UpdateStatus().AvailableVersion != "" {
		t.Fatal("up-to-date check retained a stale offer")
	}
}

func TestUpdatePreferencesRejectCorruptionAndOutsideLinks(t *testing.T) {
	for _, data := range []string{"broken", "null", "[]", "{\"automaticChecks\":null}", "{\"automaticDownloads\":\"yes\"}", "{\"lastCheck\":\"broken\"}", "{\"automaticChecks\":true,\"automaticChecks\":false}"} {
		root := t.TempDir()
		file := filepath.Join(root, "updater.json")
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readUpdateChoices(root); err == nil {
			t.Fatalf("corrupt update settings accepted: %s", data)
		}
		saved, _ := os.ReadFile(file)
		if string(saved) != data {
			t.Fatal("corrupt data was overwritten")
		}
	}
	for _, name := range []string{"updater.json", "updater.json.tmp"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside.json")
			if err := os.WriteFile(outside, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, name)); err != nil {
				t.Skip("symlinks unavailable")
			}
			if _, err := readUpdateChoices(root); err == nil {
				t.Fatal("outside link accepted")
			}
		})
	}
}

func TestDisabledUpdaterPreservesCorruptPreferences(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "updater.json")
	data := []byte("broken")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	d := &Desktop{root: root}
	// This test binary has no embedded update feed. Validation still applies
	// before the plugin can later load and overwrite its state.
	if err := d.InitializeUpdates(); err == nil {
		t.Fatal("disabled updater skipped corrupt state validation")
	}
	if err := d.CheckForUpdates(); err == nil {
		t.Fatal("disabled updater started manual check")
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != string(data) {
		t.Fatal("corrupt settings changed")
	}
}
