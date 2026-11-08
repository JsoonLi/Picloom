//go:build !windows

package main

func windowMaximized(_ int) (bool, error) { return false, nil }
