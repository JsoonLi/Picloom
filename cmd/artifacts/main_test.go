package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerArchitectureNames(t *testing.T) {
	m := metadata{Name: "Picloom", Slug: "picloom", Version: "0.3.0", Out: "dist-native"}
	cases := map[string]string{"windows/amd64": "Picloom-Setup-0.3.0-x64.exe", "darwin/amd64": "Picloom-0.3.0-macOS-x64.dmg", "darwin/arm64": "Picloom-0.3.0-macOS-arm64.dmg"}
	for platform, expected := range cases {
		_, actual, err := installerNames(m, platform)
		if err != nil || actual != expected {
			t.Fatalf("%s: %s, %v", platform, actual, err)
		}
	}
	if _, _, err := installerNames(m, "darwin/x64"); err == nil {
		t.Fatal("accepted an invalid Go architecture")
	}
}

func TestVersionMismatchAndEmptyInstallerAreRejected(t *testing.T) {
	root := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("mygo.json", `{"name":"Picloom","slug":"picloom","version":"0.3.0","out":"dist-native"}`)
	write("package.json", `{"version":"0.2.0"}`)
	if _, err := readMetadata(root); err == nil {
		t.Fatal("accepted version drift")
	}
	write("package.json", `{"version":"0.3.0"}`)
	write("dist-native/windows-amd64/picloom-setup-0.3.0-amd64.exe", "")
	if err := collect(root, "windows/amd64"); err == nil {
		t.Fatal("accepted empty artifact")
	}
}

func TestCollectCopiesOnlyTheSelectedInstaller(t *testing.T) {
	root := t.TempDir()
	m := `{"name":"Picloom","slug":"picloom","version":"0.3.0","out":"dist-native"}`
	_ = os.WriteFile(filepath.Join(root, "mygo.json"), []byte(m), 0600)
	_ = os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"version":"0.3.0"}`), 0600)
	dir := filepath.Join(root, "dist-native", "windows-amd64")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	want := []byte("synthetic installer fixture")
	if err := os.WriteFile(filepath.Join(dir, "picloom-setup-0.3.0-amd64.exe"), want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := collect(root, "windows/amd64"); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(root, "dist-native", "releases", "Picloom-Setup-0.3.0-x64.exe"))
	if err != nil || string(actual) != string(want) {
		t.Fatalf("copied artifact %q: %v", actual, err)
	}
}
