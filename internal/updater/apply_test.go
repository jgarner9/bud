package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarGz builds a release archive holding files
func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()

	return buf.Bytes()
}

// sshsign makes an armored SSHSIG over msg the way ssh-keygen -Y sign does,
// returning the key's authorized_keys line too
func sshsign(t *testing.T, msg []byte, namespace string) (string, []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	wire := func(parts ...[]byte) []byte {
		var b bytes.Buffer
		for _, p := range parts {
			writeString(&b, p)
		}
		return b.Bytes()
	}
	pubWire := wire([]byte("ssh-ed25519"), pub)
	h := sha512.Sum512(msg)

	var signed bytes.Buffer
	signed.WriteString(sshsigMagic)
	signed.Write(wire([]byte(namespace), nil, []byte("sha512"), h[:]))
	sig := ed25519.Sign(priv, signed.Bytes())

	var blob bytes.Buffer
	blob.WriteString(sshsigMagic)
	blob.Write([]byte{0, 0, 0, 1})
	blob.Write(wire(pubWire, []byte(namespace), nil, []byte("sha512"), wire([]byte("ssh-ed25519"), sig)))

	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(pubWire),
		pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: blob.Bytes()})
}

// fakeRelease serves assets and returns a Release pointing at them
func fakeRelease(t *testing.T, assets map[string][]byte) *Release {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := assets[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)

	rel := &Release{Version: "9.9.9"}
	for name := range assets {
		rel.Assets = append(rel.Assets, Asset{Name: name, URL: srv.URL + "/" + name})
	}

	return rel
}

func TestFetch(t *testing.T) {
	archive := tarGz(t, map[string]string{"README.md": "hi", "bud": "#!/bin/sh\necho new\n"})
	name := ArchiveName("linux", "arm64")
	sum := sha256.Sum256(archive)
	sums := []byte(fmt.Sprintf("%x  %s\n%x  other.tar.gz\n", sum, name, sha256.Sum256(nil)))
	key, sig := sshsign(t, sums, SigNamespace)
	_, otherSig := sshsign(t, sums, SigNamespace)

	origKey := ReleaseKey
	t.Cleanup(func() { ReleaseKey = origKey })
	logf := func(string, ...any) {}

	tests := []struct {
		name    string
		key     string
		assets  map[string][]byte
		wantErr string
	}{
		{"no key pinned, checksum only", "", map[string][]byte{name: archive, ChecksumsName: sums}, ""},
		{"signed", key, map[string][]byte{name: archive, ChecksumsName: sums, SignatureName: sig}, ""},
		{"signed by someone else", key, map[string][]byte{name: archive, ChecksumsName: sums, SignatureName: otherSig}, "different key"},
		{"key pinned, no signature", key, map[string][]byte{name: archive, ChecksumsName: sums}, "has no checksums.txt.sig"},
		{"archive changed", "", map[string][]byte{name: append(archive, 0), ChecksumsName: sums}, "checksum mismatch"},
		{"not listed", "", map[string][]byte{name: archive, ChecksumsName: []byte("abc  x\n")}, "lists no checksum"},
		{"no archive", "", map[string][]byte{ChecksumsName: sums}, "has no " + name},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ReleaseKey = tt.key
			dir := t.TempDir()
			bin, err := Fetch(context.Background(), fakeRelease(t, tt.assets), "linux", "arm64", dir, logf)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(bin)
			if string(b) != "#!/bin/sh\necho new\n" {
				t.Fatalf("extracted %q", b)
			}
			if info, _ := os.Stat(bin); info.Mode().Perm()&0o100 == 0 {
				t.Fatal("not executable")
			}
		})
	}
}

func TestExtractBudMissing(t *testing.T) {
	err := extractBud(tarGz(t, map[string]string{"bin/bud": "x"}), filepath.Join(t.TempDir(), "bud"))
	if err == nil || !strings.Contains(err.Error(), "no bud binary") {
		t.Fatalf("got %v", err)
	}
}

func TestSwap(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "bud")
	prev := filepath.Join(dir, "share", "bud.prev")
	newBin := filepath.Join(dir, "new")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("old"), 0o755)
	os.WriteFile(newBin, []byte("new"), 0o644)

	if err := Swap(newBin, target, prev); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{target: "new", prev: "old"} {
		b, _ := os.ReadFile(path)
		info, _ := os.Stat(path)
		if string(b) != want || info.Mode().Perm() != 0o755 {
			t.Errorf("%s: %q %v, want %q 0755", path, b, info.Mode(), want)
		}
	}
	// no temp files left next to either
	for _, d := range []string{filepath.Dir(target), filepath.Dir(prev)} {
		if entries, _ := os.ReadDir(d); len(entries) != 1 {
			t.Errorf("%s has %d entries", d, len(entries))
		}
	}
}
