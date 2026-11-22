package kb

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
)

// Curator is the git identity of the curator's commits, so a commit by
// anyone else to a page counts as Jason's edit
const (
	CuratorName  = "Bud curator"
	CuratorEmail = "curator@bud.invalid"
)

// Repo is a clone of bud-kb
type Repo struct {
	Dir string
}

// Git runs git in the clone and returns its trimmed stdout; on failure the
// error carries git's own message
func (r Repo) Git(ctx context.Context, args ...string) (string, error) {
	out, err := r.gitRaw(ctx, nil, args...)
	return strings.TrimSpace(string(out)), err
}

func (r Repo) gitRaw(ctx context.Context, stdout io.Writer, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Dir
	// never stop to ask for a password: the curator and the site run unattended
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s in %s: %s", args[0], r.Dir, msg)
	}

	return out.Bytes(), nil
}

// Lock takes the clone's lock, which the curator and the site share so a
// pull never lands in the middle of a curator run. It waits for the other
// holder
func (r Repo) Lock() (unlock func(), err error) {
	gitDir := filepath.Join(r.Dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return nil, fmt.Errorf("%s isn't a git clone of bud-kb: clone it there first", r.Dir)
	}
	f, err := os.OpenFile(filepath.Join(gitDir, "bud-kb.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("lock %s: %w", f.Name(), err)
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Clean reports whether the working tree has no uncommitted changes
func (r Repo) Clean(ctx context.Context) (bool, error) {
	out, err := r.Git(ctx, "status", "--porcelain")
	return out == "", err
}

// HasRemote reports whether the clone has an origin to pull from and push to
func (r Repo) HasRemote(ctx context.Context) bool {
	_, err := r.Git(ctx, "remote", "get-url", "origin")
	return err == nil
}

// Head is the commit checked out, or empty in a repo with no commits yet
func (r Repo) Head(ctx context.Context) (string, error) {
	out, err := r.Git(ctx, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		if _, e := r.Git(ctx, "rev-parse", "--git-dir"); e == nil {
			return "", nil
		}
	}

	return out, err
}

// Pull fast-forwards the clone from origin, or says it can't
func (r Repo) Pull(ctx context.Context) error {
	if !r.HasRemote(ctx) {
		return nil
	}
	if _, err := r.Git(ctx, "pull", "--ff-only", "--quiet"); err != nil {
		return fmt.Errorf("%w: the clone and origin have diverged or can't be reached; settle it by hand, nothing was changed", err)
	}

	return nil
}

// Push sends the clone's commits to origin, when it has one
func (r Repo) Push(ctx context.Context) error {
	if !r.HasRemote(ctx) {
		return nil
	}
	_, err := r.Git(ctx, "push", "--quiet", "origin", "HEAD")
	return err
}

// Commit commits the given paths (added, changed or removed) as the curator
func (r Repo) Commit(ctx context.Context, message string, paths ...string) error {
	args := append([]string{"add", "-A", "--"}, paths...)
	if _, err := r.Git(ctx, args...); err != nil {
		return err
	}
	_, err := r.Git(ctx, "-c", "user.name="+CuratorName, "-c", "user.email="+CuratorEmail,
		"commit", "--quiet", "--no-verify", "--author", CuratorName+" <"+CuratorEmail+">", "-m", message)
	if err != nil {
		// leave the tree as the last commit had it, so the next run starts clean
		_, _ = r.Git(ctx, append([]string{"reset", "--quiet", "--"}, paths...)...)
		_, _ = r.Git(ctx, append([]string{"checkout", "--quiet", "HEAD", "--"}, paths...)...)
	}

	return err
}

// Discard puts paths back as HEAD has them, removing ones HEAD lacks
func (r Repo) Discard(ctx context.Context, paths ...string) {
	for _, p := range paths {
		if _, err := r.Git(ctx, "cat-file", "-e", "HEAD:"+p); err == nil {
			_, _ = r.Git(ctx, "checkout", "--quiet", "HEAD", "--", p)
		} else {
			_ = os.Remove(filepath.Join(r.Dir, filepath.FromSlash(p)))
		}
	}
}

// LastHumanEdit is when someone other than the curator last committed to
// the file, or the zero time when nobody has
func (r Repo) LastHumanEdit(ctx context.Context, file string) (time.Time, error) {
	out, err := r.Git(ctx, "log", "--format=%ae%x09%aI", "--", file)
	if err != nil {
		return time.Time{}, err
	}
	for l := range strings.SplitSeq(out, "\n") {
		email, date, ok := strings.Cut(l, "\t")
		if !ok || email == CuratorEmail {
			continue
		}
		return time.Parse(time.RFC3339, date)
	}

	return time.Time{}, nil
}

// LastCommit is when anyone, the curator included, last committed to the
// file, or the zero time when nobody has
func (r Repo) LastCommit(ctx context.Context, file string) (time.Time, error) {
	out, err := r.Git(ctx, "log", "-1", "--format=%aI", "--", file)
	if err != nil || out == "" {
		return time.Time{}, err
	}

	return time.Parse(time.RFC3339, out)
}

// Export writes the content/ and data/ trees of commit into dst, so a build
// sees only what was committed
func (r Repo) Export(ctx context.Context, commit, dst string) error {
	var tarball bytes.Buffer
	if _, err := r.gitRaw(ctx, &tarball, "archive", "--format=tar", commit); err != nil {
		return err
	}
	tr := tar.NewReader(&tarball)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the archive of %s: %w", commit, err)
		}
		name := path.Clean(h.Name)
		if !strings.HasPrefix(name, "content/") && !strings.HasPrefix(name, "data/") {
			continue
		}
		if !fs.ValidPath(name) {
			return fmt.Errorf("%s holds a path outside the tree: %q", commit, h.Name)
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			b, err := readAll(tr)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if err := os.WriteFile(target, b, 0o644); err != nil {
				return err
			}
		default:
			// symlinks and the like could point outside the build: refuse them
			return fmt.Errorf("%s: %s isn't a plain file; the KB holds only pages", commit, name)
		}
	}
}

// ReadPage reads the page at rel (a slash path inside the KB)
func (r Repo) ReadPage(rel string) (*Page, error) {
	f, err := os.Open(filepath.Join(r.Dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := readAll(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	p, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}

	return p, nil
}

// WritePage writes the page at rel
func (r Repo) WritePage(rel string, p *Page) error {
	b, err := p.Bytes()
	if err != nil {
		return err
	}
	target := filepath.Join(r.Dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	return atomicfile.WriteFile(target, b, 0o644)
}

// Pages lists the project and system pages, by path, sorted. It doesn't
// parse them: callers read each with ReadPage, which fails on a page that
// doesn't parse, so the curator never rewrites what it can't read
func (r Repo) Pages() ([]string, error) {
	var out []string
	for _, dir := range Kinds {
		entries, err := os.ReadDir(filepath.Join(r.Dir, "content", dir))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".md") && e.Name() != "_index.md" {
				out = append(out, "content/"+dir+"/"+e.Name())
			}
		}
	}
	sort.Strings(out)

	return out, nil
}

// QueuePath is the review queue's file, which the site reads as site.Data.review
const QueuePath = "data/review.json"

// Claim is one side of a review item
type Claim struct {
	Value     string   `json:"value"`
	Statement string   `json:"statement,omitempty"`
	Sources   []Source `json:"sources,omitempty"`
}

// Item kinds on the review queue
const (
	ItemContradiction = "contradiction"
	ItemRefresh       = "refresh"
)

// Item is one thing waiting on Jason
type Item struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Page    string `json:"page"`
	Section string `json:"section"`
	// Key is the fact in question, for a contradiction
	Key string `json:"key,omitempty"`
	// Current is what the page says, Proposed what new memory says
	Current  *Claim `json:"current,omitempty"`
	Proposed *Claim `json:"proposed,omitempty"`
	Opened   string `json:"opened"`
	Note     string `json:"note,omitempty"`
}

// ItemID is a short id that stays the same for the same question, so a
// rerun finds the item it already queued
func ItemID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:8]
}

// Queue is the review queue
type Queue struct {
	Items []Item `json:"items"`
}

// Find returns the item with id, or nil
func (q *Queue) Find(id string) *Item {
	for i := range q.Items {
		if q.Items[i].ID == id {
			return &q.Items[i]
		}
	}

	return nil
}

// Remove drops the item with id
func (q *Queue) Remove(id string) {
	out := q.Items[:0]
	for _, it := range q.Items {
		if it.ID != id {
			out = append(out, it)
		}
	}
	q.Items = out
}

// ReadQueue reads the review queue; none yet is an empty one
func (r Repo) ReadQueue() (*Queue, error) {
	b, err := os.ReadFile(filepath.Join(r.Dir, filepath.FromSlash(QueuePath)))
	if errors.Is(err, fs.ErrNotExist) {
		return &Queue{Items: []Item{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var q Queue
	if err := json.Unmarshal(b, &q); err != nil {
		return nil, fmt.Errorf("%s: %w", QueuePath, err)
	}
	if q.Items == nil {
		q.Items = []Item{}
	}

	return &q, nil
}

// WriteQueue writes the review queue, sorted by when items were opened
func (r Repo) WriteQueue(q *Queue) error {
	sort.SliceStable(q.Items, func(i, j int) bool {
		if q.Items[i].Opened != q.Items[j].Opened {
			return q.Items[i].Opened < q.Items[j].Opened
		}
		return q.Items[i].ID < q.Items[j].ID
	})
	b, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(r.Dir, filepath.FromSlash(QueuePath))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	return atomicfile.WriteFile(target, append(b, '\n'), 0o644)
}
