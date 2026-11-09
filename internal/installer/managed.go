package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"jgarner.dev/bud/internal/paths"
)

// MinNeovim is the oldest Neovim bud supports (D19)
var MinNeovim = [2]int{0, 12}

// MinTreeSitter is the oldest tree-sitter CLI nvim-treesitter's main branch builds parsers with
var MinTreeSitter = [3]int{0, 26, 1}

// Asset is one downloadable build of a Tool. SHA256 is pinned here, in the
// signed bud binary (D7), so a download is only trusted when it matches
type Asset struct {
	URL    string
	SHA256 string
	// Member is the file to link as the tool's binary: a path inside the
	// archive, or "" for a single gzip-compressed file
	Member string
}

// Tool is a dependency bud downloads itself on Linux when the package manager's
// is missing or too old (D107). Debian stable, for one, ships a Neovim a
// release or two behind what bud needs. macOS has Homebrew, which doesn't
type Tool struct {
	Name    string
	Bin     string
	Version string
	// Min is the oldest version that counts as installed
	Min [3]int
	// Assets is keyed by GOARCH
	Assets map[string]Asset
}

// Pinned versions. Bumping one means new URLs and digests (GitHub shows the
// digest of every release asset)
var (
	neovimTool = Tool{
		Name: "neovim", Bin: "nvim", Version: "0.12.6",
		Min: [3]int{MinNeovim[0], MinNeovim[1], 0},
		Assets: map[string]Asset{
			"amd64": {
				URL:    "https://github.com/neovim/neovim/releases/download/v0.12.6/nvim-linux-x86_64.tar.gz",
				SHA256: "474430d53e6264f6d6dd18db42d6dc9df3a1b56ca9e88a325bbf860e1a811d87",
				Member: "nvim-linux-x86_64/bin/nvim",
			},
			"arm64": {
				URL:    "https://github.com/neovim/neovim/releases/download/v0.12.6/nvim-linux-arm64.tar.gz",
				SHA256: "8f1f64a0bdb97247034038c3823c6cbad5bdf9ecd5751b85494b71c3ee04c815",
				Member: "nvim-linux-arm64/bin/nvim",
			},
		},
	}
	treeSitterTool = Tool{
		Name: "tree-sitter-cli", Bin: "tree-sitter", Version: "0.27.2",
		Min: MinTreeSitter,
		Assets: map[string]Asset{
			"amd64": {
				URL:    "https://github.com/tree-sitter/tree-sitter/releases/download/v0.27.2/tree-sitter-linux-x64.gz",
				SHA256: "7ed56d152c52437397b9fcaa43cf341f49933cecc1ca291843f0d347c6e2cb3a",
			},
			"arm64": {
				URL:    "https://github.com/tree-sitter/tree-sitter/releases/download/v0.27.2/tree-sitter-linux-arm64.gz",
				SHA256: "34fcc5c4fa7bdd2058a2a45a03b6eb9a49b03a93b6d52ec750f99b1516d98d33",
			},
		},
	}
)

// swappable in tests
var (
	goarch = runtime.GOARCH
	// fetch downloads url into memory
	fetch = func(url string) ([]byte, error) {
		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s: %s", url, resp.Status)
		}

		return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
	}
	// versionOutput runs bin --version. As bud, since Neovim writes its log
	// even for --version, into the state dir its name picks
	versionOutput = func(bin string) (string, error) {
		cmd := exec.Command(bin, "--version")
		cmd.Env = append(os.Environ(), "NVIM_APPNAME="+paths.AppName)
		out, err := cmd.Output()
		return string(out), err
	}
)

// maxDownload bounds what a download may take in memory
const maxDownload = 256 << 20

// managed returns the Tool d is downloaded as on this machine, or false when
// it comes from the package manager: not Linux, or no build for this CPU
func (d Dep) managed() (Tool, bool) {
	if d.Tool == nil || goos != "linux" {
		return Tool{}, false
	}
	if _, ok := d.Tool.Assets[goarch]; !ok {
		return Tool{}, false
	}

	return *d.Tool, true
}

// Managed says whether bud downloads this dep itself on this machine
func (d Dep) Managed() bool {
	_, ok := d.managed()
	return ok
}

// newEnough says whether the t.Bin on PATH is at least t.Min
func (t Tool) newEnough() bool {
	bin, err := exec.LookPath(t.Bin)
	if err != nil {
		return false
	}

	return t.enough(bin)
}

// enough says whether the binary at bin is at least t.Min
func (t Tool) enough(bin string) bool {
	out, err := versionOutput(bin)
	if err != nil {
		return false
	}
	have, ok := ParseVersion(out)

	return ok && slices.Compare(have[:], t.Min[:]) >= 0
}

var versionRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// ParseVersion reads the first version number on out's first line, as in
// "NVIM v0.12.6" or "tree-sitter 0.27.2 (…)". A missing patch number is 0
func ParseVersion(out string) ([3]int, bool) {
	line, _, _ := strings.Cut(out, "\n")
	m := versionRe.FindString(line)
	if m == "" {
		return [3]int{}, false
	}
	var v [3]int
	for i, part := range strings.Split(m, ".") {
		v[i], _ = strconv.Atoi(part)
	}

	return v, true
}

// ManagedBin is where the downloaded tools' binaries are linked, a dir bud puts
// first on PATH (UseManagedBin)
func ManagedBin(data string) string { return filepath.Join(data, "bin") }

// managedDir holds each downloaded tool unpacked, one dir per version
func managedDir(data string) string { return filepath.Join(data, "managed") }

// UseManagedBin puts bud's downloaded tools ahead of the system's on PATH, for
// this process and everything it starts (nvim, Mason, the Claude Code session).
// It does nothing when nothing was downloaded
func UseManagedBin(data string) {
	dir := ManagedBin(data)
	if _, err := os.Stat(dir); err != nil {
		return
	}
	path := os.Getenv("PATH")
	for _, p := range filepath.SplitList(path) {
		if p == dir {
			return
		}
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+path)
}

// PruneManaged unlinks a downloaded tool once the system has one new enough
// (the user installed Neovim 0.13 since), so it stops shadowing the system's.
// The unpacked copy stays. It runs from 'bud install', not at startup, because
// it starts the tools to read their versions
func PruneManaged(data string) {
	dir := ManagedBin(data)
	for _, d := range Deps {
		t, ok := d.managed()
		if !ok {
			continue
		}
		name := filepath.Join(dir, t.Bin)
		if _, err := os.Lstat(name); err != nil {
			continue
		}
		if sys := lookPathOutside(t.Bin, dir); sys != "" && t.enough(sys) {
			os.Remove(name)
		}
	}
}

// lookPathOutside finds bin on PATH, skipping the directory skip
func lookPathOutside(bin, skip string) string {
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d == "" || d == skip {
			continue
		}
		p := filepath.Join(d, bin)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode().Perm()&0o111 != 0 {
			return p
		}
	}

	return ""
}

// Download installs t for this machine under data and links its binary into
// ManagedBin. It checks the archive against the pinned digest before reading
// anything from it
func (t Tool) Download(data string) error {
	asset, ok := t.Assets[goarch]
	if !ok {
		return fmt.Errorf("%s: no build for %s", t.Name, goarch)
	}
	body, err := fetch(asset.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", t.Name, err)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != asset.SHA256 {
		return fmt.Errorf("%s: checksum mismatch for %s (want %s, got %s); nothing was installed", t.Name, asset.URL, asset.SHA256, got)
	}

	if err := os.MkdirAll(managedDir(data), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(managedDir(data), ".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	member := asset.Member
	if member == "" {
		// a single gzip-compressed binary
		member = t.Bin
		if err := gunzipTo(body, filepath.Join(tmp, member)); err != nil {
			return fmt.Errorf("unpack %s: %w", t.Name, err)
		}
	} else if err := untar(body, tmp); err != nil {
		return fmt.Errorf("unpack %s: %w", t.Name, err)
	}

	final := filepath.Join(managedDir(data), t.Name+"-"+t.Version)
	// move an earlier copy aside rather than deleting it first, so a failure
	// leaves the link pointing at something that works
	aside := final + ".old"
	if err := os.RemoveAll(aside); err != nil {
		return err
	}
	replaced := true
	if err := os.Rename(final, aside); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		replaced = false
	}
	if err := os.Rename(tmp, final); err != nil {
		if replaced {
			os.Rename(aside, final)
		}
		return err
	}
	defer os.RemoveAll(aside)

	return link(filepath.Join(final, member), filepath.Join(ManagedBin(data), t.Bin))
}

// gunzipTo writes the decompressed body to path as an executable
func gunzipTo(body []byte, path string) error {
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer zr.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(zr, maxDownload)); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

// untar unpacks a gzipped tar into dir, refusing anything that would land
// outside it
func untar(body []byte, dir string) error {
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.Clean(hdr.Name))
		if !within(dir, target) {
			return fmt.Errorf("%q is outside the archive", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			err = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			err = writeFile(target, tr, hdr.FileInfo().Mode().Perm()|0o600)
		case tar.TypeSymlink:
			// a relative link that stays in the archive
			if filepath.IsAbs(hdr.Linkname) || !within(dir, filepath.Join(filepath.Dir(target), hdr.Linkname)) {
				return fmt.Errorf("%q links outside the archive", hdr.Name)
			}
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err == nil {
				err = os.Symlink(hdr.Linkname, target)
			}
		default:
			// hard links, devices and the like aren't in these archives
			return fmt.Errorf("%q has an unsupported type (%c)", hdr.Name, hdr.Typeflag)
		}
		if err != nil {
			return err
		}
	}
}

func within(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(r, maxDownload)); err != nil {
		f.Close()
		return err
	}

	return f.Close()
}

// link points name at target, replacing a link from an earlier version in one step
func link(target, name string) error {
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("the archive has no %s", filepath.Base(target))
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	tmp := name + ".new"
	os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}

	return os.Rename(tmp, name)
}
