package updater

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"jgarner.dev/bud/internal/version"
)

// SourceBranch is the branch `bud update --from-main` builds
const SourceBranch = "main"

// SourceURL is where --from-main fetches the source. Swappable in tests
var SourceURL = "https://github.com/" + Repo + ".git"

// versionPkg is the package whose Version variable release builds stamp
const versionPkg = "jgarner.dev/bud/internal/version"

// FetchMain brings the checkout in dir to the tip of main and returns its
// full commit hash. dir is bud's own cache of the source (a shallow clone), so
// it's reset and cleaned freely; the first call clones and later ones only
// fetch the new commits
func FetchMain(ctx context.Context, dir string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("building from main needs git on PATH: install git, then run bud update --from-main again")
	}

	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", err
		}
		if _, err := run(ctx, "", nil, "git", "clone", "--quiet", "--depth", "1", "--branch", SourceBranch, SourceURL, dir); err != nil {
			return "", err
		}
	} else {
		steps := [][]string{
			{"fetch", "--quiet", "--depth", "1", SourceURL, SourceBranch},
			{"reset", "--quiet", "--hard", "FETCH_HEAD"},
			{"clean", "--quiet", "-fdx"},
		}
		for _, args := range steps {
			if _, err := run(ctx, dir, nil, "git", args...); err != nil {
				return "", fmt.Errorf("%w (delete %s to start the checkout again)", err, dir)
			}
		}
	}

	return run(ctx, dir, nil, "git", "rev-parse", "HEAD")
}

// SourceBase is the version a source build is a prerelease of: the checkout's
// VERSION (the last stable release) with the patch raised by one, as the
// nightly's is
func SourceBase(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return "", fmt.Errorf("read the source's version: %w", err)
	}
	s, ok := version.Parse(strings.TrimSpace(string(b)))
	if !ok || s.Pre != nil {
		return "", fmt.Errorf("the source's VERSION file holds %q, not a release version like 1.2.3", strings.TrimSpace(string(b)))
	}

	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch+1), nil
}

// SourceVersion names a source build X.Y.Z-nightly.YYYYMMDDHHMM.main.<commit>.
// It reads as a nightly built at that time, so a later nightly or release
// replaces it by the usual comparison and bud treats it like one; the trailing
// main.<commit> says what it was built from
func SourceVersion(base string, built time.Time, commit string) string {
	return fmt.Sprintf("%s-nightly.%s.%s.%s", base, built.UTC().Format("200601021504"), SourceBranch, ShortCommit(commit))
}

// ShortCommit is the 7-character form of a commit hash
func ShortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}

	return commit
}

// SourceCommit returns the commit a source build named by SourceVersion was
// built from, or "" for any other version
func SourceCommit(v string) string {
	s, ok := version.Parse(v)
	if !ok || len(s.Pre) != 4 || s.Pre[0] != "nightly" || s.Pre[2] != SourceBranch {
		return ""
	}
	if _, err := strconv.Atoi(s.Pre[1]); err != nil {
		return ""
	}

	return s.Pre[3]
}

// BuildMain compiles the checkout in dir into out, as a release build named v:
// the same flags .goreleaser.yaml uses, so it's the binary a release would be
// minus its signature
func BuildMain(ctx context.Context, dir, out, v string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("building from main needs Go on PATH: install it from https://go.dev/dl, then run bud update --from-main again")
	}
	ldflags := "-s -w -X " + versionPkg + ".Version=" + v
	if _, err := run(ctx, dir, []string{"CGO_ENABLED=0"}, "go", "build", "-trimpath", "-ldflags", ldflags, "-o", out, "./cmd/bud"); err != nil {
		return fmt.Errorf("building bud from main: %w", err)
	}

	return nil
}

// run runs a command in dir (the current one when empty) and returns its
// trimmed stdout; on failure the error carries the command's own message
func run(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	// never stop to ask for a password: this may run unattended
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", name, args[0], msg)
	}

	return strings.TrimSpace(out.String()), nil
}
