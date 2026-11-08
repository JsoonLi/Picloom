package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updatewindow "github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"image2ai/internal/application"
	"image2ai/internal/desktop"
	"image2ai/internal/domain"
	"image2ai/internal/storage"
	"image2ai/internal/studio"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := userDataRoot(os.Getenv("IMAGE2AI_USER_DATA"))
	if err != nil {
		return fmt.Errorf("数据目录无效：%w", err)
	}
	// Keep the established data and lock location when the app is renamed.
	mygo.App.SetPath(mygo.PathUserData, root)
	mygo.App.SetName("Picloom")
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	var window *mygo.Window
	var rootErr error
	var store *storage.Store
	var studioApp *studio.App
	var stopUpdateEvents func()
	var closeOnce sync.Once
	closeStore := func() {
		closeOnce.Do(func() {
			if stopUpdateEvents != nil {
				stopUpdateEvents()
			}
			if studioApp != nil {
				studioApp.Close()
			}
			if store != nil {
				_ = store.Close()
			}
		})
	}
	defer closeStore()
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		if window != nil {
			window.Restore()
			window.Focus()
		}
	})
	// Complete data ownership before registering any updater lifecycle handler.
	var backend *application.Backend
	var config domain.Config
	var system *desktop.Desktop
	startupErr := func() error {
		if err := desktop.CheckOtherInstances(); err != nil {
			return err
		}
		store, err = storage.Open(root)
		if err != nil {
			return err
		}
		if err = store.BackupLegacy(); err != nil {
			return err
		}
		backend = application.New(store)
		config, err = backend.LoadConfig()
		if err != nil {
			return err
		}
		system = desktop.New(backend, root)
		return system.ValidateUpdateSettings()
	}()
	if startupErr != nil {
		mygo.App.WhenReady(func() {
			mygo.Dialog.Error("Picloom 无法启动", startupErr.Error())
			mygo.App.Quit()
		})
		if err := mygo.App.Run(); err != nil {
			return err
		}
		return startupErr
	}
	mygo.App.WhenReady(func() {
		fail := func(err error) {
			rootErr = err
			mygo.Dialog.Error("Picloom 无法启动", err.Error())
			mygo.App.Quit()
		}
		if err := system.InitializeUpdates(); err != nil {
			fail(err)
			return
		}
		system.SetTheme(config.Theme)
		studioApp = studio.New(backend, system)
		stopUpdateEvents = updater.OnChange(studioApp.RefreshUpdates)
		if runtime.GOOS == "darwin" {
			checkUpdates := updater.MenuItem()
			checkUpdates.Disabled = !mygo.Updater.Enabled()
			checkUpdates.Click = func(*mygo.MenuItem, *mygo.Window) {
				go func() {
					if err := system.CheckForUpdates(); err != nil {
						mygo.RunOnMain(func() { mygo.Dialog.Error("无法检查更新", err.Error()) })
					}
				}()
			}
			mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
				{Label: "Picloom", Submenu: []*mygo.MenuItem{
					{Role: mygo.RoleAbout}, checkUpdates, mygo.Separator(),
					{Role: mygo.RoleServices}, mygo.Separator(),
					{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide},
					mygo.Separator(), {Role: mygo.RoleQuit},
				}},
				{Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu},
			}))
		}
		window = mygo.NewWindow(mygo.WindowOptions{Title: "Picloom · 绘织", Width: 1280, Height: 880, MinWidth: 740, MinHeight: 560, StateKey: "main", TitleBarStyle: mygo.TitleBarHidden, HideWindowButtons: runtime.GOOS == "windows", Content: ui.View(studioApp.View)})
		system.SetWindow(window)
		studioApp.SetWindow(window)
		mygo.App.OnWillQuit(func(_ *mygo.QuitEvent) { closeStore() })
	})
	// Register after the studio's ready handler: data ownership and migration
	// checks must complete before the plugin starts its background work.
	mygo.Use(updatewindow.New(updater.Options{Language: "zh-Hans", Strings: map[string]updater.Strings{
		"zh-Hans": {ReadyMessage: "%[1]s %[2]s 已安装，下次启动时生效。立即重启会结束当前任务，请先保存草稿。", Relaunch: "立即重启"},
	}}))
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return rootErr
}

func userDataRoot(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Image2AI"), nil
}
