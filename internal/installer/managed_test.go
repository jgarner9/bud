package installer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type tarEntry struct {
	name, body, link string
	typ              byte
	mode             int64
}

func makeTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Typeflag: typ, Mode: e.mode, Linkname: e.link}
		if typ == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	zw.Close()

	return buf.Bytes()
}

func makeGz(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(body))
	zw.Close()

	return buf.Bytes()
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// withFetch serves body for any URL and swaps the arch to amd64
func withFetch(t *testing.T, body []byte) {
	t.Helper()
	origFetch, origArch := fetch, goarch
	fetch = func(string) ([]byte, error) { return body, nil }
	goarch = "amd64"
	t.Cleanup(func() { fetch, goarch = origFetch, origArch })
}

func withVersion(t *testing.T, out string, err error) {
	t.Helper()
	orig := versionOutput
	versionOutput = func(string) (string, error) { return out, err }
	t.Cleanup(func() { versionOutput = orig })
}

func nvimTarGz(t *testing.T) []byte {
	return makeTarGz(t,
		tarEntry{name: "nvim-linux-x86_64/", typ: tar.TypeDir, mode: 0o755},
		tarEntry{name: "nvim-linux-x86_64/bin/nvim", body: "#!/bin/sh\necho NVIM v0.12.6\n", mode: 0o755},
		tarEntry{name: "nvim-linux-x86_64/share/nvim/runtime/init.vim", body: "\" runtime\n", mode: 0o644},
	)
}

func TestPinnedToolsMeetTheMinimums(t *testing.T) {
	for _, tool := range []Tool{neovimTool, treeSitterTool} {
		v, ok := ParseVersion(tool.Version)
		if !ok || slices.Compare(v[:], tool.Min[:]) < 0 {
			t.Errorf("%s %s is older than the minimum %v", tool.Name, tool.Version, tool.Min)
		}
		for arch, a := range tool.Assets {
			if !strings.Contains(a.URL, tool.Version) {
				t.Errorf("%s %s: url %s names another version", tool.Name, arch, a.URL)
			}
			if len(a.SHA256) != 64 {
				t.Errorf("%s %s: sha256 %q isn't a digest", tool.Name, arch, a.SHA256)
			}
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in   string
		want [3]int
		ok   bool
	}{
		{"NVIM v0.12.6\nBuild type: Release\n", [3]int{0, 12, 6}, true},
		{"NVIM v0.10.4-dev+g1234", [3]int{0, 10, 4}, true},
		{"tree-sitter 0.27.2 (abcdef 2026-11-01)", [3]int{0, 27, 2}, true},
		{"tree-sitter 0.26", [3]int{0, 26, 0}, true},
		{"", [3]int{}, false},
		{"no digits here\nNVIM v0.12.6", [3]int{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseVersion(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseVersion(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDownloadUnpacksAndLinks(t *testing.T) {
	archive := nvimTarGz(t)
	withFetch(t, archive)
	data := t.TempDir()
	tool := neovimTool
	tool.Assets = map[string]Asset{"amd64": {URL: "https://example.test/nvim.tar.gz", SHA256: digest(archive), Member: "nvim-linux-x86_64/bin/nvim"}}

	if err := tool.Download(data); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(ManagedBin(data), "nvim")
	if out, err := os.ReadFile(bin); err != nil || !strings.Contains(string(out), "NVIM v0.12.6") {
		t.Fatalf("read through the link: %q, %v", out, err)
	}
	if fi, err := os.Stat(bin); err != nil || fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary should be executable: %v, %v", fi, err)
	}
	// the runtime sits beside the binary, where nvim looks for it
	runtime := filepath.Join(managedDir(data), "neovim-0.12.6", "nvim-linux-x86_64", "share", "nvim", "runtime", "init.vim")
	if _, err := os.Stat(runtime); err != nil {
		t.Fatalf("runtime not unpacked: %v", err)
	}
	// no staging dirs left behind
	entries, _ := os.ReadDir(managedDir(data))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("left %s behind", e.Name())
		}
	}
}

func TestDownloadReplacesAnOlderLink(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(data, "elsewhere", "nvim")
	os.MkdirAll(filepath.Dir(old), 0o755)
	os.WriteFile(old, []byte("old"), 0o755)
	os.MkdirAll(ManagedBin(data), 0o755)
	if err := os.Symlink(old, filepath.Join(ManagedBin(data), "nvim")); err != nil {
		t.Fatal(err)
	}

	archive := nvimTarGz(t)
	withFetch(t, archive)
	tool := neovimTool
	tool.Assets = map[string]Asset{"amd64": {URL: "u", SHA256: digest(archive), Member: "nvim-linux-x86_64/bin/nvim"}}
	if err := tool.Download(data); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(ManagedBin(data), "nvim")); !strings.Contains(string(got), "v0.12.6") {
		t.Fatalf("link still points at the old binary: %q", got)
	}
}

func TestDownloadSingleGzipFile(t *testing.T) {
	archive := makeGz(t, "#!/bin/sh\necho tree-sitter 0.27.2\n")
	withFetch(t, archive)
	data := t.TempDir()
	tool := treeSitterTool
	tool.Assets = map[string]Asset{"amd64": {URL: "u", SHA256: digest(archive)}}

	if err := tool.Download(data); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(ManagedBin(data), "tree-sitter"))
	if err != nil || fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("want an executable tree-sitter: %v, %v", fi, err)
	}
}

func TestDownloadRefusesWhatDoesNotMatchTheDigest(t *testing.T) {
	archive := nvimTarGz(t)
	withFetch(t, archive)
	data := t.TempDir()
	tool := neovimTool
	tool.Assets = map[string]Asset{"amd64": {URL: "u", SHA256: strings.Repeat("0", 64), Member: "nvim-linux-x86_64/bin/nvim"}}

	err := tool.Download(data)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v, want a checksum mismatch", err)
	}
	if _, err := os.Stat(data + "/managed"); !os.IsNotExist(err) {
		t.Fatal("nothing should have been written before the digest matched")
	}
}

func TestDownloadReportsFetchErrors(t *testing.T) {
	origFetch, origArch := fetch, goarch
	fetch = func(string) ([]byte, error) { return nil, errors.New("503 Service Unavailable") }
	goarch = "amd64"
	t.Cleanup(func() { fetch, goarch = origFetch, origArch })

	err := neovimTool.Download(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "download neovim") || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadRefusesUnsafeArchives(t *testing.T) {
	tests := map[string][]tarEntry{
		"path out of the archive": {{name: "../escape", body: "x", mode: 0o644}},
		"absolute symlink":        {{name: "nvim/bin/vi", typ: tar.TypeSymlink, link: "/usr/bin/nvim"}},
		"symlink out of the dir":  {{name: "nvim/bin/vi", typ: tar.TypeSymlink, link: "../../../../etc/passwd"}},
		"hard link":               {{name: "nvim/bin/vi", typ: tar.TypeLink, link: "nvim/bin/nvim"}},
	}
	for name, bad := range tests {
		t.Run(name, func(t *testing.T) {
			// a real binary first, so the only reason to fail is the unsafe entry
			entries := append([]tarEntry{{name: "nvim/bin/nvim", body: "#!/bin/sh\n", mode: 0o755}}, bad...)
			archive := makeTarGz(t, entries...)
			withFetch(t, archive)
			data := t.TempDir()
			tool := neovimTool
			tool.Assets = map[string]Asset{"amd64": {URL: "u", SHA256: digest(archive), Member: "nvim/bin/nvim"}}

			if err := tool.Download(data); err == nil {
				t.Fatal("want an error")
			}
			if _, err := os.Lstat(filepath.Join(ManagedBin(data), "nvim")); err == nil {
				t.Error("nothing should be linked after a refused archive")
			}
			if _, err := os.Stat(filepath.Join(managedDir(data), "escape")); err == nil {
				t.Error("a file escaped the archive")
			}
		})
	}
}

func TestDownloadMissingMember(t *testing.T) {
	archive := nvimTarGz(t)
	withFetch(t, archive)
	tool := neovimTool
	tool.Assets = map[string]Asset{"amd64": {URL: "u", SHA256: digest(archive), Member: "nvim-linux-x86_64/bin/missing"}}

	if err := tool.Download(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no missing") {
		t.Fatalf("err = %v", err)
	}
}

func TestManagedOnlyOnLinuxWithABuildForThisCPU(t *testing.T) {
	dep := Dep{Name: "neovim", Bins: []string{"nvim"}, Tool: &neovimTool}
	origArch := goarch
	t.Cleanup(func() { goarch = origArch })

	tests := []struct {
		os, arch string
		want     bool
	}{
		{"linux", "amd64", true},
		{"linux", "arm64", true},
		{"linux", "riscv64", false},
		{"darwin", "arm64", false},
	}
	for _, tt := range tests {
		withGOOS(t, tt.os)
		goarch = tt.arch
		if got := dep.Managed(); got != tt.want {
			t.Errorf("%s/%s: Managed() = %v, want %v", tt.os, tt.arch, got, tt.want)
		}
	}
	if (Dep{Name: "git", Bins: []string{"git"}}).Managed() {
		t.Error("a dep with no Tool is never managed")
	}
}

func TestInstalledChecksTheVersionOfADownloadedTool(t *testing.T) {
	withGOOS(t, "linux")
	withFetch(t, nil)
	fakePath(t, "nvim")
	dep := Dep{Name: "neovim", Bins: []string{"nvim"}, Tool: &neovimTool}

	tests := []struct {
		out  string
		err  error
		want bool
	}{
		{"NVIM v0.10.4\n", nil, false},
		{"NVIM v0.11.9\n", nil, false},
		{"NVIM v0.12.0\n", nil, true},
		{"NVIM v0.13.1\n", nil, true},
		{"NVIM v1.0.0\n", nil, true},
		{"garbage", nil, false},
		{"", errors.New("exec format error"), false},
	}
	for _, tt := range tests {
		withVersion(t, tt.out, tt.err)
		if got := dep.Installed(); got != tt.want {
			t.Errorf("nvim %q (err %v): Installed() = %v, want %v", tt.out, tt.err, got, tt.want)
		}
	}

	// on macOS an old one still counts: Homebrew's is current, and bud doesn't upgrade
	withGOOS(t, "darwin")
	withVersion(t, "NVIM v0.10.4\n", nil)
	if !dep.Installed() {
		t.Error("macOS only needs the binary to be there")
	}

	// and absent is absent
	withGOOS(t, "linux")
	t.Setenv("PATH", t.TempDir())
	if dep.Installed() {
		t.Error("no nvim on PATH should not be installed")
	}
}

func TestTreeSitterMinimumIsTheBuildersNotTheMajor(t *testing.T) {
	withGOOS(t, "linux")
	withFetch(t, nil)
	fakePath(t, "tree-sitter")
	dep := Dep{Name: "tree-sitter-cli", Bins: []string{"tree-sitter"}, Tool: &treeSitterTool}

	for out, want := range map[string]bool{
		"tree-sitter 0.22.6\n":       false,
		"tree-sitter 0.26.0\n":       false,
		"tree-sitter 0.26.1\n":       true,
		"tree-sitter 0.27.2 (abc)\n": true,
	} {
		withVersion(t, out, nil)
		if got := dep.Installed(); got != want {
			t.Errorf("%q: Installed() = %v, want %v", out, got, want)
		}
	}
}

func TestPlanDownloadsInsteadOfUsingThePackageManagerOnLinux(t *testing.T) {
	withRoot(t, false)
	withGOOS(t, "linux")
	withFetch(t, nil)
	withDeps(t, []Dep{
		{Name: "neovim", Bins: []string{"nvim"}, Tool: &neovimTool},
		{Name: "git", Bins: []string{"git"}},
	})
	// Debian: apt's nvim is on PATH but too old, and git is missing
	fakePath(t, "nvim", "apt-get")
	withVersion(t, "NVIM v0.10.4\n", nil)

	plan := NewPlan(Needed("minimal"))
	if got := plan.Command(); !slices.Equal(got, []string{"sudo", "apt-get", "install", "-y", "git"}) {
		t.Fatalf("command = %q, want only git from apt", got)
	}
	if dl := plan.Downloads(); len(dl) != 1 || dl[0].Name != "neovim" {
		t.Fatalf("downloads = %v, want neovim", dl)
	}

	// on macOS the same dep comes from Homebrew
	withGOOS(t, "darwin")
	fakePath(t, "brew")
	plan = NewPlan(Needed("minimal"))
	if len(plan.Downloads()) != 0 {
		t.Fatalf("downloads on macOS = %v, want none", plan.Downloads())
	}
	if got := plan.Command(); !slices.Equal(got, []string{"brew", "install", "neovim", "git"}) {
		t.Fatalf("command on macOS = %q", got)
	}
}

func TestPlanRunDownloadsThenPutsItFirstOnPATH(t *testing.T) {
	withRoot(t, false)
	withGOOS(t, "linux")
	archive := nvimTarGz(t)
	withFetch(t, archive)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	tool := neovimTool
	tool.Assets = map[string]Asset{"amd64": {URL: "u", SHA256: digest(archive), Member: "nvim-linux-x86_64/bin/nvim"}}
	withDeps(t, []Dep{{Name: "neovim", Bins: []string{"nvim"}, Tool: &tool}})
	// nothing from the package manager runs, and none is needed
	withRun(t, func([]string) error { t.Fatal("the package manager should not run"); return nil })
	fakePath(t)
	// the version of whatever is first on PATH: the old system one before, ours after
	origVersion := versionOutput
	versionOutput = func(bin string) (string, error) {
		if strings.HasPrefix(bin, home) {
			return "NVIM v0.12.6\n", nil
		}
		return "NVIM v0.10.4\n", nil
	}
	t.Cleanup(func() { versionOutput = origVersion })

	plan := NewPlan(Needed("minimal"))
	if len(plan.Missing) != 1 {
		t.Fatalf("missing = %v", plan.Missing)
	}
	if err := plan.Run(); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "share", "bud", "bin")
	if first := filepath.SplitList(os.Getenv("PATH"))[0]; first != want {
		t.Fatalf("first on PATH = %q, want %q", first, want)
	}
}

func TestPlanRunStopsOnADownloadFailure(t *testing.T) {
	withRoot(t, false)
	withGOOS(t, "linux")
	archive := nvimTarGz(t)
	withFetch(t, archive)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	tool := neovimTool
	tool.Assets = map[string]Asset{"amd64": {URL: "u", SHA256: strings.Repeat("f", 64)}}
	withDeps(t, []Dep{
		{Name: "neovim", Bins: []string{"nvim"}, Tool: &tool},
		{Name: "git", Bins: []string{"git"}},
	})
	fakePath(t, "apt-get")
	withRun(t, func([]string) error { t.Fatal("nothing should install after a bad download"); return nil })

	err := NewPlan(Needed("minimal")).Run()
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestUseManagedBin(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PATH", "/usr/bin:/bin")

	UseManagedBin(data)
	if os.Getenv("PATH") != "/usr/bin:/bin" {
		t.Fatalf("PATH changed with nothing downloaded: %q", os.Getenv("PATH"))
	}

	os.MkdirAll(ManagedBin(data), 0o755)
	UseManagedBin(data)
	UseManagedBin(data)
	if want := ManagedBin(data) + ":/usr/bin:/bin"; os.Getenv("PATH") != want {
		t.Fatalf("PATH = %q, want %q (once, first)", os.Getenv("PATH"), want)
	}
}

// Neovim writes nvim.log even for --version, into the state dir NVIM_APPNAME
// picks, so the probe has to run as bud or it litters ~/.local/state/nvim (JGA-224)
func TestVersionOutputRunsAsBud(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "appname")
	script := "#!/bin/sh\nprintf '%s' \"$NVIM_APPNAME\" > " + record + "\necho NVIM v0.12.6\n"
	bin := filepath.Join(dir, "nvim")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NVIM_APPNAME", "someone-elses")

	out, err := versionOutput(bin)
	if err != nil || !strings.Contains(out, "v0.12.6") {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if got, _ := os.ReadFile(record); string(got) != "bud" {
		t.Fatalf("ran with NVIM_APPNAME=%q, want bud", got)
	}
}
