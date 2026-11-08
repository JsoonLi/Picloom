//go:build !windows

package storage

import (
	"errors"
	"os"
	"syscall"
)

func acquireLock(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, errOutside
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func replaceFile(old, new string) error { return os.Rename(old, new) }
func transientReplace(err error) bool {
	return errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
