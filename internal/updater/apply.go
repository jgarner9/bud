package updater

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"jgarner.dev/bud/internal/version"
)

const (
	ChecksumsName = "checksums.txt"
	SignatureName = "checksums.txt.sig"

	// a release archive is ~10 MB; anything near this is not one
	maxDownload = 256 << 20
)

// swappable in tests: archives take longer than an API call
var downloadClient = &http.Client{Timeout: 10 * time.Minute}

// Fetch downloads the release's archive for goos/goarch, checks it (D7) and
// extracts the bud binary into dir, returning its path. The signature on
// checksums.txt is checked when this build pins a release key; until one is
// pinned (D41) only the SHA-256 is, like install.sh (D43)
func Fetch(ctx context.Context, r *Release, goos, goarch, dir string, logf func(string, ...any)) (string, error) {
	name := ArchiveName(goos, goarch)
	sums, err := fetchAsset(ctx, r, ChecksumsName)
	if err != nil {
		return "", err
	}
	if ReleaseKey == "" {
		logf("no release key pinned in this build; checking the SHA-256 only")
	} else {
		sig, err := fetchAsset(ctx, r, SignatureName)
		if err != nil {
			return "", err
		}
		if err := VerifyRelease(sums, sig); err != nil {
			return "", fmt.Errorf("%s %s: %w", r.Version, ChecksumsName, err)
		}
		logf("signature ok")
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return "", err
	}

	archive, err := fetchAsset(ctx, r, name)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("checksum mismatch for %s (want %s, got %x)", name, want, got)
	}
	logf("checksum ok")

	bin := filepath.Join(dir, "bud")
	if err := extractBud(archive, bin); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}

	return bin, nil
}

func fetchAsset(ctx context.Context, r *Release, name string) ([]byte, error) {
	a, ok := r.Asset(name)
	if !ok {
		return nil, fmt.Errorf("release %s has no %s", r.Version, name)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "bud/"+version.String())

	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", name, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", name, err)
	}
	if len(b) > maxDownload {
		return nil, fmt.Errorf("downloading %s: too large", name)
	}

	return b, nil
}

// checksumFor finds name's SHA-256 in a sha256sum-style checksums.txt
func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}

	return "", fmt.Errorf("%s lists no checksum for %s", ChecksumsName, name)
}

// extractBud writes the archive's top-level bud binary to dst
func extractBud(archive []byte, dst string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("no bud binary in the archive")
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || strings.TrimPrefix(h.Name, "./") != "bud" {
			continue
		}

		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(tr, maxDownload)); err != nil {
			f.Close()
			return err
		}

		return f.Close()
	}
}

// CheckBinary runs `bin version` so a binary that won't start for this
// machine is caught before it replaces the working one
func CheckBinary(ctx context.Context, bin string) error {
	out, err := exec.CommandContext(ctx, bin, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("the downloaded bud doesn't run: %w: %s", err, bytes.TrimSpace(out))
	}

	return nil
}

// Swap keeps a copy of target at prev, then renames newBin's contents into
// place at target. The rename is atomic, so target is always a whole binary
func Swap(newBin, target, prev string) error {
	if err := copyExecutable(target, prev); err != nil {
		return fmt.Errorf("keeping the current bud as %s: %w", prev, err)
	}
	if err := copyExecutable(newBin, target); err != nil {
		return fmt.Errorf("installing the new bud at %s: %w", target, err)
	}

	return nil
}

// Restore puts the kept binary at prev back at target
func Restore(prev, target string) error {
	return copyExecutable(prev, target)
}

// copyExecutable writes src to a temp file next to dst and renames it over dst
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".bud-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmp.Name(), dst)
}
