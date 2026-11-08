// Artifacts normalizes local build filenames after verification; it never publishes.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

type metadata struct{ Name, Slug, Version, Out string }

func main() {
	target := flag.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "verified native platform")
	flag.Parse()
	if err := collect(".", *target); err != nil {
		fmt.Fprintln(os.Stderr, "整理安装包失败：", err)
		os.Exit(1)
	}
}

func readMetadata(root string) (metadata, error) {
	var m metadata
	data, err := os.ReadFile(filepath.Join(root, "mygo.json"))
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	var pkg struct{ Version string }
	data, err = os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(data, &pkg); err != nil {
		return m, err
	}
	if m.Version != pkg.Version {
		return m, errors.New("mygo.json 与 package.json 版本不一致")
	}
	if !regexp.MustCompile(`^[0-9]+[.][0-9]+[.][0-9]+$`).MatchString(m.Version) || m.Name != "Image2AI" || m.Slug != "image2ai" || m.Out != "dist-native" {
		return m, errors.New("应用元数据或输出位置无效")
	}
	return m, nil
}

func installerNames(m metadata, target string) (source, destination string, err error) {
	switch target {
	case "windows/amd64":
		return filepath.Join(m.Out, "windows-amd64", "image2ai-setup-"+m.Version+"-amd64.exe"), "Image2AI-Setup-" + m.Version + "-x64.exe", nil
	case "darwin/amd64":
		return filepath.Join(m.Out, "darwin-amd64", "image2ai-"+m.Version+"-amd64.dmg"), "Image2AI-" + m.Version + "-macOS-x64.dmg", nil
	case "darwin/arm64":
		return filepath.Join(m.Out, "darwin-arm64", "image2ai-"+m.Version+"-arm64.dmg"), "Image2AI-" + m.Version + "-macOS-arm64.dmg", nil
	default:
		return "", "", errors.New("不支持的安装包平台")
	}
}

func collect(root, target string) error {
	m, err := readMetadata(root)
	if err != nil {
		return err
	}
	source, name, err := installerNames(m, target)
	if err != nil {
		return err
	}
	input, err := os.Open(filepath.Join(root, source))
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("安装包为空或不是普通文件")
	}
	output := filepath.Join(root, m.Out, "releases")
	if err = os.MkdirAll(output, 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(output, ".installer-*")
	if err != nil {
		return err
	}
	tempName := temporary.Name()
	defer os.Remove(tempName)
	digest := sha256.New()
	if _, err = io.Copy(io.MultiWriter(temporary, digest), input); err != nil {
		temporary.Close()
		return err
	}
	if err = temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	destination := filepath.Join(output, name)
	if err = os.Rename(tempName, destination); err != nil {
		return err
	}
	fmt.Printf("%s  %x\n", destination, digest.Sum(nil))
	return nil
}
