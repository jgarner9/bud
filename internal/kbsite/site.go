// Package kbsite builds the KB site from bud-kb's committed pages with
// Hugo and serves it on the tailnet (Suite D53, D122). A build sees only
// what HEAD holds, and replaces the live site only when Hugo succeeds, every
// internal link resolves and nothing looks like a secret, so a bad commit
// leaves the last good site up. See docs/internals/kb.md
package kbsite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/kb"
)

// Hugo runs hugo over the site in src, writing it to dst
type Hugo func(ctx context.Context, src, dst string) error

// ExecHugo runs the hugo binary at bin
func ExecHugo(bin string) Hugo {
	return func(ctx context.Context, src, dst string) error {
		cmd := exec.CommandContext(ctx, bin, "--source", src, "--destination", dst, "--quiet")
		// no module downloads or other network: the site is all local
		cmd.Env = append(os.Environ(), "HUGO_ENVIRONMENT=production", "HUGO_MODULE_PROXY=off")
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if errors.Is(err, exec.ErrNotFound) {
				msg = "hugo isn't installed: install Hugo 0.146 or newer (server/hugo/README.md)"
			}
			return fmt.Errorf("hugo: %s", msg)
		}
		return nil
	}
}

// Builder builds the site for one KB clone into Dir
type Builder struct {
	Repo kb.Repo
	// Site is the Hugo config and layouts (bud.KBSite())
	Site fs.FS
	Hugo Hugo
	// Dir holds the builds, current (a link to the live one) and status.json
	Dir string
}

// Status is what the last build did, for bud doctor and Jason
type Status struct {
	// Commit is the KB commit the live site shows
	Commit  string    `json:"commit,omitempty"`
	BuiltAt time.Time `json:"built_at,omitzero"`
	// Failed is the last commit that didn't build, and why; empty once one builds
	Failed      string    `json:"failed,omitempty"`
	FailedAt    time.Time `json:"failed_at,omitzero"`
	Error       string    `json:"error,omitempty"`
	LastChecked time.Time `json:"last_checked,omitzero"`
}

// Current is the live site's directory, a link the builder swaps
func (b *Builder) Current() string { return filepath.Join(b.Dir, "current") }

func (b *Builder) statusPath() string { return filepath.Join(b.Dir, "status.json") }

// ReadStatus reads the last build's status; none yet is the zero Status
func (b *Builder) ReadStatus() (Status, error) {
	var s Status
	data, err := os.ReadFile(b.statusPath())
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}

func (b *Builder) writeStatus(s Status) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(b.statusPath(), append(data, '\n'), 0o644)
}

// retryFailed is how long a commit that failed to build waits before it's tried again
const retryFailed = time.Hour

// Build builds HEAD when it isn't what the live site shows, or force is
// set, and swaps it in only when it passes. It returns whether the live
// site changed
func (b *Builder) Build(ctx context.Context, force bool) (bool, error) {
	if err := os.MkdirAll(b.Dir, 0o755); err != nil {
		return false, err
	}
	st, err := b.ReadStatus()
	if err != nil {
		return false, err
	}
	head, err := b.Repo.Head(ctx)
	if err != nil {
		return false, err
	}
	if head == "" {
		return false, fmt.Errorf("%s has no commits yet: commit a first page", b.Repo.Dir)
	}
	st.LastChecked = time.Now().UTC()
	// a commit that failed is tried again after an hour, in case what it
	// needed (hugo, say) has been fixed since
	if !force && (head == st.Commit || head == st.Failed && time.Since(st.FailedAt) < retryFailed) {
		return false, b.writeStatus(st)
	}

	out, err := b.build(ctx, head)
	if err != nil {
		st.Failed, st.FailedAt, st.Error = head, time.Now().UTC(), err.Error()
		if werr := b.writeStatus(st); werr != nil {
			return false, werr
		}
		return false, fmt.Errorf("build %s: %w (the live site still shows %s)", short(head), err, orNone(short(st.Commit)))
	}
	if err := b.swap(out); err != nil {
		return false, err
	}
	b.prune(filepath.Base(out))
	st.Commit, st.BuiltAt, st.Failed, st.FailedAt, st.Error = head, time.Now().UTC(), "", time.Time{}, ""

	return true, b.writeStatus(st)
}

func short(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func orNone(s string) string {
	if s == "" {
		return "nothing"
	}
	return s
}

// build makes the site for commit in a fresh directory and checks it
func (b *Builder) build(ctx context.Context, commit string) (string, error) {
	src, err := os.MkdirTemp(b.Dir, "src-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(src)
	if err := os.CopyFS(src, b.Site); err != nil {
		return "", fmt.Errorf("copy the site's layouts: %w", err)
	}
	if err := b.Repo.Export(ctx, commit, src); err != nil {
		return "", err
	}
	if err := scanSecrets(src, "content", "data"); err != nil {
		return "", err
	}
	out := filepath.Join(b.Dir, "site-"+short(commit)+"-"+time.Now().UTC().Format("20060102T150405.000000000"))
	if err := b.Hugo(ctx, src, out); err != nil {
		os.RemoveAll(out)
		return "", err
	}
	if err := checkSite(out); err != nil {
		os.RemoveAll(out)
		return "", err
	}

	return out, nil
}

// swap points current at out in one rename, so a request never sees half a site
func (b *Builder) swap(out string) error {
	tmp := b.Current() + ".new"
	_ = os.Remove(tmp)
	if err := os.Symlink(filepath.Base(out), tmp); err != nil {
		return err
	}
	return os.Rename(tmp, b.Current())
}

// prune keeps the live build and the one before it, for a quick look back
func (b *Builder) prune(live string) {
	entries, err := os.ReadDir(b.Dir)
	if err != nil {
		return
	}
	var builds []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "site-") && e.Name() != live {
			builds = append(builds, e.Name())
		}
	}
	// names end in the build time, so the newest sort last
	sort.Slice(builds, func(i, j int) bool { return buildTime(builds[i]) < buildTime(builds[j]) })
	for len(builds) > 1 {
		_ = os.RemoveAll(filepath.Join(b.Dir, builds[0]))
		builds = builds[1:]
	}
}

func buildTime(name string) string { return name[strings.LastIndex(name, "-")+1:] }

// secretRes match text that looks like a credential. The KB should hold
// only op:// references, never values (Suite D7)
var secretRes = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{40,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{30,}`),
	regexp.MustCompile(`\bops_[A-Za-z0-9_-]{40,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\blin_api_[A-Za-z0-9]{30,}`),
}

// scanSecrets fails on the first file under root/dirs that looks like it holds a secret
func scanSecrets(root string, dirs ...string) error {
	for _, d := range dirs {
		base := filepath.Join(root, d)
		err := filepath.WalkDir(base, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && p == base {
					return nil
				}
				return err
			}
			if !e.Type().IsRegular() {
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, re := range secretRes {
				if re.Match(data) {
					rel, _ := filepath.Rel(root, p)
					return fmt.Errorf("%s looks like it holds a secret (it matches %s): replace the value with an op:// reference and commit again", filepath.ToSlash(rel), re)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

var linkRe = regexp.MustCompile(`(?i)\b(?:href|src)="([^"]*)"`)

// checkSite checks the built site: a home page, no secrets, and every
// internal link landing on a file the build made
func checkSite(out string) error {
	if _, err := os.Stat(filepath.Join(out, "index.html")); err != nil {
		return errors.New("the build made no home page")
	}
	if err := scanSecrets(out, "."); err != nil {
		return err
	}
	var broken []string
	err := filepath.WalkDir(out, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(out, p)
		page := "/" + filepath.ToSlash(rel)
		for _, m := range linkRe.FindAllSubmatch(data, -1) {
			if target, ok := internal(page, string(m[1])); ok && !exists(out, target) {
				broken = append(broken, page+" → "+string(m[1]))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(broken) > 0 {
		sort.Strings(broken)
		if len(broken) > 5 {
			broken = append(broken[:5], fmt.Sprintf("and %d more", len(broken)-5))
		}
		return fmt.Errorf("broken links: %s", strings.Join(broken, "; "))
	}

	return nil
}

// internal resolves a link on page to a site path, or reports it leaves the site
func internal(page, link string) (string, bool) {
	link = strings.ReplaceAll(link, "&amp;", "&")
	if i := strings.IndexAny(link, "#?"); i >= 0 {
		link = link[:i]
	}
	if link == "" || strings.Contains(link, ":") || strings.HasPrefix(link, "//") {
		return "", false
	}
	if !strings.HasPrefix(link, "/") {
		link = path.Join(path.Dir(page), link)
	}

	return path.Clean(link), true
}

func exists(out, target string) bool {
	p := filepath.Join(out, filepath.FromSlash(strings.TrimPrefix(target, "/")))
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	if info.IsDir() {
		_, err = os.Stat(filepath.Join(p, "index.html"))
	}
	return err == nil
}

// Handler serves the live site, reading current afresh on each request so a
// swap takes effect at once
func (b *Builder) Handler() http.Handler {
	files := http.FileServer(http.Dir(b.Current()))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "the KB site is read-only", http.StatusMethodNotAllowed)
			return
		}
		if _, err := os.Stat(b.Current()); err != nil {
			http.Error(w, "the KB site hasn't been built yet: check bud kb-site's log", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// Watch pulls the clone (when pull is set) and rebuilds on a new commit,
// every interval until ctx ends. A failed pull or build is logged and the
// live site stays
func (b *Builder) Watch(ctx context.Context, interval time.Duration, pull bool, logf func(string, ...any)) {
	tick := func() {
		if pull {
			unlock, err := b.Repo.Lock()
			if err != nil {
				logf("kb-site: %v", err)
				return
			}
			err = b.Repo.Pull(ctx)
			unlock()
			if err != nil {
				logf("kb-site: %v", err)
			}
		}
		changed, err := b.Build(ctx, false)
		switch {
		case err != nil:
			logf("kb-site: %v", err)
		case changed:
			st, _ := b.ReadStatus()
			logf("kb-site: now showing %s", short(st.Commit))
		}
	}
	tick()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
