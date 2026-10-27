package updater

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testdata holds signatures made with real ssh-keygen and a throwaway key
// (D41): test.pub signed checksums.txt under bud-release (sha512 and sha256),
// and under the wrong namespace; an ecdsa key signed it too
func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func TestVerifySSHSig(t *testing.T) {
	msg := readTestdata(t, "checksums.txt")
	key := strings.TrimSpace(string(readTestdata(t, "test.pub")))
	other := strings.TrimSpace(string(readTestdata(t, "other.pub")))

	tests := []struct {
		name, key, sig, ns string
		msg                []byte
		wantErr            string
	}{
		{"sha512", key, "checksums.txt.sig", SigNamespace, msg, ""},
		{"sha256", key, "checksums.txt.sha256.sig", SigNamespace, msg, ""},
		{"changed file", key, "checksums.txt.sig", SigNamespace, append([]byte("x"), msg...), "doesn't match"},
		{"other key", other, "checksums.txt.sig", SigNamespace, msg, "different key"},
		{"wrong namespace", key, "checksums.txt.file-ns.sig", SigNamespace, msg, "namespace"},
		{"ecdsa signature", key, "checksums.txt.ecdsa.sig", SigNamespace, msg, "isn't ssh-ed25519"},
		{"bad public key", "ssh-rsa AAAA", "checksums.txt.sig", SigNamespace, msg, "not an ssh-ed25519"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := VerifySSHSig(tt.key, tt.ns, tt.msg, readTestdata(t, tt.sig))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestVerifySSHSigGarbage(t *testing.T) {
	key := strings.TrimSpace(string(readTestdata(t, "test.pub")))
	good := string(readTestdata(t, "checksums.txt.sig"))
	for name, sig := range map[string]string{
		"empty":     "",
		"not armor": "hello",
		"truncated": strings.Replace(good, "ozssoCJe69M6NswebklmOewuJ9sS9fBw==", "", 1),
		"wrong pem": strings.ReplaceAll(good, "SSH SIGNATURE", "PUBLIC KEY"),
	} {
		if err := VerifySSHSig(key, SigNamespace, []byte("x"), []byte(sig)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestVerifyReleaseNeedsKey(t *testing.T) {
	orig := ReleaseKey
	t.Cleanup(func() { ReleaseKey = orig })

	ReleaseKey = ""
	if err := VerifyRelease(nil, nil); !errors.Is(err, ErrNoReleaseKey) {
		t.Fatalf("want ErrNoReleaseKey, got %v", err)
	}

	ReleaseKey = strings.TrimSpace(string(readTestdata(t, "test.pub")))
	if err := VerifyRelease(readTestdata(t, "checksums.txt"), readTestdata(t, "checksums.txt.sig")); err != nil {
		t.Fatal(err)
	}
}

// A fresh key and signature from the ssh-keygen on this machine, so a change
// in OpenSSH's output shows up here, not only in the fixtures
func TestVerifySSHSigLiveKeygen(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "k")
	msgPath := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(msgPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-q", "-t", "ed25519", "-N", "", "-f", keyPath},
		{"-q", "-Y", "sign", "-f", keyPath, "-n", SigNamespace, msgPath},
	} {
		if out, err := exec.Command("ssh-keygen", args...).CombinedOutput(); err != nil {
			t.Skipf("ssh-keygen %v: %v: %s", args, err, out)
		}
	}
	pub, _ := os.ReadFile(keyPath + ".pub")
	sig, _ := os.ReadFile(msgPath + ".sig")
	if err := VerifySSHSig(string(pub), SigNamespace, []byte("hello\n"), sig); err != nil {
		t.Fatal(err)
	}
}
