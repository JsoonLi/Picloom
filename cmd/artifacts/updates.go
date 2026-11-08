package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// collectUpdate verifies the bytes being copied, before exposing a manifest
// to the release uploader. No signing material is required by this verifier.
func collectUpdate(root string, m metadata, target, output string) error {
	public, err := base64.StdEncoding.DecodeString(strings.TrimSpace(m.Updates.PublicKey))
	if err != nil || len(public) != ed25519.PublicKeySize || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(m.Updates.GitHub) {
		return errors.New("更新签名公钥或 GitHub 仓库无效")
	}
	platform := strings.ReplaceAll(target, "/", "-")
	directory := filepath.Join(root, m.Out, platform)
	name := m.Slug + "-" + m.Version + "-" + platform + ".tar.gz"
	manifestName := "update-" + platform + ".json"
	file, err := os.Open(filepath.Join(directory, manifestName))
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	file.Close()
	if err != nil {
		return err
	}
	var manifest struct {
		Version, URL, Signature string
		Size                    int64
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &manifest) != nil {
		return errors.New("更新清单无效")
	}
	expectedURL := "https://github.com/" + m.Updates.GitHub + "/releases/download/v" + m.Version + "/" + name
	signature, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil || manifest.Version != m.Version || manifest.URL != expectedURL || manifest.Size <= 0 || len(signature) != ed25519.SignatureSize {
		return errors.New("更新清单版本、平台、下载地址或签名不匹配")
	}
	archive, err := os.Open(filepath.Join(directory, name))
	if err != nil {
		return err
	}
	defer archive.Close()
	info, err := archive.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != manifest.Size {
		return errors.New("更新包大小不匹配")
	}
	digest := sha256.New()
	if err := verifiedCopy(output, name, io.TeeReader(io.LimitReader(archive, manifest.Size+1), digest), func(n int64) error {
		if n != manifest.Size || !ed25519.Verify(public, digest.Sum(nil), signature) {
			return errors.New("更新包数字签名验证失败")
		}
		return nil
	}); err != nil {
		return err
	}
	return verifiedCopy(output, manifestName, bytes.NewReader(data), nil)
}

func verifiedCopy(directory, name string, input io.Reader, verify func(int64) error) error {
	temporary, err := os.CreateTemp(directory, ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	n, err := io.Copy(temporary, input)
	if err != nil {
		return err
	}
	if verify != nil {
		if err := verify(n); err != nil {
			return err
		}
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary.Name(), filepath.Join(directory, name)); err != nil {
		return err
	}
	fmt.Printf("%s (verified)\n", filepath.Join(directory, name))
	return nil
}
