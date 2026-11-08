package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenamedAppKeepsLegacyUserDataRoot(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	root, err := userDataRoot("")
	if err != nil || root != filepath.Join(base, "Image2AI") {
		t.Fatalf("renaming must preserve the existing data directory: %v", err)
	}
	isolated := t.TempDir()
	root, err = userDataRoot(isolated)
	if err != nil || root != isolated {
		t.Fatalf("isolated verification override lost: %v", err)
	}
}
