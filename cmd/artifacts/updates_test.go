package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func signedArtifactFixture(t *testing.T, change func(map[string]any), tamper bool) string {
	t.Helper()
	root := t.TempDir()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("synthetic signed application archive")
	digest := sha256.Sum256(data)
	manifest := map[string]any{"version": "0.6.0", "url": "https://github.com/JsoonLi/Picloom/releases/download/v0.6.0/picloom-0.6.0-windows-amd64.tar.gz", "size": len(data), "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(private, digest[:]))}
	if change != nil {
		change(manifest)
	}
	meta := map[string]any{"name": "Picloom", "slug": "picloom", "version": "0.6.0", "out": "dist-native", "updates": map[string]any{"publicKey": base64.StdEncoding.EncodeToString(public), "github": "JsoonLi/Picloom"}}
	write := func(path string, data []byte) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	encoded, _ := json.Marshal(meta)
	write("mygo.json", encoded)
	write("package.json", []byte("{\"version\":\"0.6.0\"}"))
	write("dist-native/windows-amd64/picloom-setup-0.6.0-amd64.exe", []byte("synthetic installer"))
	encoded, _ = json.Marshal(manifest)
	write("dist-native/windows-amd64/update-windows-amd64.json", encoded)
	if tamper {
		data = append(data, '!')
	}
	write("dist-native/windows-amd64/picloom-0.6.0-windows-amd64.tar.gz", data)
	return root
}

func TestCollectSignedUpdateArtifacts(t *testing.T) {
	root := signedArtifactFixture(t, nil, false)
	if err := collect(root, "windows/amd64"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Picloom-Setup-0.6.0-x64.exe", "picloom-0.6.0-windows-amd64.tar.gz", "update-windows-amd64.json"} {
		data, err := os.ReadFile(filepath.Join(root, "dist-native/releases", name))
		if err != nil || len(data) == 0 {
			t.Fatalf("missing verified release artifact %s: %v", name, err)
		}
	}
}

func TestCollectRejectsUntrustedUpdateArtifacts(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
		tamper bool
	}{
		{"modified archive", nil, true},
		{"wrong version", func(m map[string]any) { m["version"] = "9.0.0" }, false},
		{"wrong architecture", func(m map[string]any) {
			m["url"] = "https://github.com/JsoonLi/Picloom/releases/download/v0.6.0/picloom-0.6.0-darwin-arm64.tar.gz"
		}, false},
		{"untrusted origin", func(m map[string]any) { m["url"] = "https://example.test/archive.tar.gz" }, false},
		{"invalid signature", func(m map[string]any) {
			m["signature"] = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := signedArtifactFixture(t, tc.change, tc.tamper)
			if err := collect(root, "windows/amd64"); err == nil {
				t.Fatal("untrusted update was accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "dist-native/releases/update-windows-amd64.json")); !os.IsNotExist(err) {
				t.Fatal("untrusted manifest was published")
			}
		})
	}
}
