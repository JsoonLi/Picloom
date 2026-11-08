//go:build windows

package storage

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func acquireLock(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errOutside
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
func replaceFile(old, new string) error {
	source, err := syscall.UTF16PtrFromString(old)
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(new)
	if err != nil {
		return err
	}
	// MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH. Both files are on
	// the same volume; on failure the old destination remains in place.
	result, _, callErr := moveFileEx.Call(uintptr(unsafe.Pointer(source)), uintptr(unsafe.Pointer(target)), 0x1|0x8)
	if result == 0 {
		return callErr
	}
	return nil
}
func transientReplace(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33))
}
func syncDirectory(string) error { return nil }
