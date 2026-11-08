package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"image2ai/internal/application"
	"image2ai/internal/desktop"
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
	var closeOnce sync.Once
	closeStore := func() {
		closeOnce.Do(func() {
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
	mygo.App.WhenReady(func() {
		fail := func(err error) {
			rootErr = err
			mygo.Dialog.Error("Picloom 无法启动", err.Error())
			mygo.App.Quit()
		}
		root, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			fail(fmt.Errorf("无法打开数据目录：%w", err))
			return
		}
		// A different framework's single-instance lock cannot protect these files.
		if err = desktop.CheckOtherInstances(); err != nil {
			fail(err)
			return
		}
		store, err = storage.Open(root)
		if err != nil {
			fail(err)
			return
		}
		if err = store.BackupLegacy(); err != nil {
			fail(err)
			return
		}
		backend := application.New(store)
		config, err := backend.LoadConfig()
		if err != nil {
			fail(err)
			return
		}
		system := desktop.New(backend, root)
		system.SetTheme(config.Theme)
		studioApp = studio.New(backend, system)
		if runtime.GOOS == "darwin" {
			mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}, {Role: mygo.RoleWindowMenu}}))
		}
		window = mygo.NewWindow(mygo.WindowOptions{Title: "Picloom · 绘织", Width: 1280, Height: 880, MinWidth: 740, MinHeight: 560, StateKey: "main", TitleBarStyle: mygo.TitleBarHidden, HideWindowButtons: runtime.GOOS == "windows", Content: ui.View(studioApp.View)})
		system.SetWindow(window)
		studioApp.SetWindow(window)
		mygo.App.OnWillQuit(func(_ *mygo.QuitEvent) { closeStore() })
	})
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
