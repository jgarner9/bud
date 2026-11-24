// Package resume starts the next OpenCode orchestrator session after one hands
// off (JGA-394). A session that fills its context queues its resume prompt
// (bud resume queue); a timer runs bud resume run, which claims the prompt
// once and starts the next session in a detached tmux session, so Jason can
// attach and answer its asks. See docs/internals/resume.md
package resume

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/atomicfile"
)

// Version is the queue file's format
const Version = 1

// MaxPrompt caps a resume prompt; a handoff prompt is a few paragraphs
const MaxPrompt = 64 << 10

// Entry is one queued resume prompt. The consumed copy also records when
// the runner claimed it and how starting it went
type Entry struct {
	Version  int       `json:"version"`
	QueuedAt time.Time `json:"queued_at"`
	Dir      string    `json:"dir"`             // where the next session starts: a checkout
	Agent    string    `json:"agent,omitempty"` // the OpenCode agent, empty for the default
	Prompt   string    `json:"prompt"`

	ClaimedAt  *time.Time `json:"claimed_at,omitempty"`
	LaunchedAt *time.Time `json:"launched_at,omitempty"` // bud resume launch, once opencode has started
	Error      string     `json:"error,omitempty"`
}

// Started says whether launch reached opencode
func (e *Entry) Started() bool { return e.LaunchedAt != nil && e.Error == "" }

// Validate checks what queue and run both rely on
func (e *Entry) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("queue format %d, this bud reads %d", e.Version, Version)
	}
	if strings.TrimSpace(e.Prompt) == "" {
		return errors.New("the prompt is empty")
	}
	if len(e.Prompt) > MaxPrompt {
		return fmt.Errorf("the prompt is %d bytes, over the %d-byte limit", len(e.Prompt), MaxPrompt)
	}
	if !filepath.IsAbs(e.Dir) {
		return fmt.Errorf("dir %q isn't an absolute path", e.Dir)
	}
	if st, err := os.Stat(e.Dir); err != nil || !st.IsDir() {
		return fmt.Errorf("dir %s isn't a directory", e.Dir)
	}
	if strings.HasPrefix(e.Agent, "-") {
		return fmt.Errorf("agent %q can't start with -", e.Agent)
	}

	return nil
}

// Queue is the state dir that holds the pending prompt, the consumed ones,
// the kill switch and the runner's lock
type Queue struct{ Dir string }

func (q Queue) pending() string  { return filepath.Join(q.Dir, "pending.json") }
func (q Queue) consumed() string { return filepath.Join(q.Dir, "consumed") }
func (q Queue) offFile() string  { return filepath.Join(q.Dir, "off") }
func (q Queue) lockFile() string { return filepath.Join(q.Dir, "lock") }

// ErrQueued is Add's error when a prompt is already waiting
var ErrQueued = errors.New("a resume prompt is already queued")

// Add queues e. It refuses when one is already queued, so two sessions
// can't both hand off and leave the runner to pick one
func (q Queue) Add(e Entry) error {
	if err := e.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(q.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(q.Dir, ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// a link fails when pending.json exists, so the check and the write are one step
	if err := os.Link(tmp.Name(), q.pending()); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrQueued
		}
		return err
	}

	return nil
}

// Pending returns the queued prompt, or nil when none is
func (q Queue) Pending() (*Entry, error) {
	return readEntry(q.pending())
}

func readEntry(path string) (*Entry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	return &e, nil
}

// Claim moves the pending prompt into consumed/, which is what makes a
// prompt run at most once: only one rename of pending.json can succeed.
// It returns nil when nothing is queued
func (q Queue) Claim(now time.Time) (*Entry, string, error) {
	if err := os.MkdirAll(q.consumed(), 0o700); err != nil {
		return nil, "", err
	}
	dst := filepath.Join(q.consumed(), now.UTC().Format("20060102T150405.000000000Z")+".json")
	if err := os.Rename(q.pending(), dst); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", err
	}
	e, err := readEntry(dst)
	if err != nil {
		return nil, dst, err
	}
	if e == nil {
		return nil, dst, fmt.Errorf("%s vanished after the claim", dst)
	}
	t := now.UTC()
	e.ClaimedAt = &t

	return e, dst, q.record(dst, e)
}

// record rewrites a consumed entry with how its start went
func (q Queue) record(path string, e *Entry) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, append(data, '\n'), 0o600)
}

// ClaimedSince counts the prompts claimed at or after t, from consumed/'s names
func (q Queue) ClaimedSince(t time.Time) (int, error) {
	names, err := os.ReadDir(q.consumed())
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range names {
		stamp, ok := strings.CutSuffix(d.Name(), ".json")
		if !ok {
			continue
		}
		at, err := time.Parse("20060102T150405.000000000Z", stamp)
		if err == nil && !at.Before(t) {
			n++
		}
	}

	return n, nil
}

// Last returns the newest consumed entry, or nil when there is none
func (q Queue) Last() (*Entry, error) {
	names, err := os.ReadDir(q.consumed())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []string
	for _, d := range names {
		if strings.HasSuffix(d.Name(), ".json") {
			files = append(files, d.Name())
		}
	}
	if len(files) == 0 {
		return nil, nil
	}
	sort.Strings(files)

	return readEntry(filepath.Join(q.consumed(), files[len(files)-1]))
}

// Off says whether the kill switch is on
func (q Queue) Off() bool {
	_, err := os.Lstat(q.offFile())
	return err == nil
}

// SetOff turns the kill switch on or off
func (q Queue) SetOff(off bool) error {
	if !off {
		if err := os.Remove(q.offFile()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(q.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(q.offFile(), []byte("bud resume is off; bud resume on starts orchestrators again\n"), 0o600)
}

// LockWait takes the runner's lock, waiting for a run that holds it, so
// off and on can't land in the middle of a start
func (q Queue) LockWait() (unlock func(), err error) {
	return q.lock(syscall.LOCK_EX)
}

func (q Queue) lock(how int) (func(), error) {
	if err := os.MkdirAll(q.Dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(q.lockFile(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Lock takes the runner's lock without waiting. ok is false when another
// runner holds it. The lock goes with the process, so a crash frees it
func (q Queue) Lock() (unlock func(), ok bool, err error) {
	unlock, err = q.lock(syscall.LOCK_EX | syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return unlock, true, nil
}

// Read reads one entry file, as bud resume launch does with a consumed one
func Read(path string) (*Entry, error) {
	e, err := readEntry(path)
	if err == nil && e == nil {
		err = fmt.Errorf("%s doesn't exist", path)
	}
	return e, err
}

// MarkLaunched records, from inside the tmux session, that opencode has
// started; the runner waits for it before calling a start good
func MarkLaunched(path string, now time.Time) error {
	e, err := Read(path)
	if err != nil {
		return err
	}
	t := now.UTC()
	e.LaunchedAt = &t
	return Queue{}.record(path, e)
}

// MarkFailed records why launch couldn't start opencode
func MarkFailed(path string, cause error) error {
	e, err := Read(path)
	if err != nil {
		return err
	}
	e.Error = cause.Error()
	return Queue{}.record(path, e)
}
