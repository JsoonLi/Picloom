//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"
)

func windowMaximized(pid int) (bool, error) {
	dll := syscall.NewLazyDLL("user32.dll")
	enum := dll.NewProc("EnumWindows")
	owner := dll.NewProc("GetWindowThreadProcessId")
	visible := dll.NewProc("IsWindowVisible")
	zoomed := dll.NewProc("IsZoomed")
	var handle uintptr
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var id uint32
		owner.Call(hwnd, uintptr(unsafe.Pointer(&id)))
		show, _, _ := visible.Call(hwnd)
		if id == uint32(pid) && show != 0 {
			handle = hwnd
			return 0
		}
		return 1
	})
	enum.Call(callback, 0)
	if handle == 0 {
		return false, errors.New("测试窗口尚未显示")
	}
	maximized, _, _ := zoomed.Call(handle)
	return maximized != 0, nil
}
