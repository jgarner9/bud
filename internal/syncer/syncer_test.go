package syncer

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/profile"
)

// signer is a throwaway ed25519 key that signs the way ssh-keygen -Y sign does
type signer struct {
	priv ed25519.PrivateKey
	pub  string
	wire []byte
}

func newSigner(t *testing.T) *signer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	w := sshString([]byte("ssh-ed25519"), pub)

	return &signer{priv: priv, wire: w, pub: "ssh-ed25519 " + base64.StdEncoding.EncodeToString(w)}
}

func sshString(parts ...[]byte) []byte {
	var b bytes.Buffer
	for _, p := range parts {
		binary.Write(&b, binary.BigEndian, uint32(len(p)))
		b.Write(p)
	}

	return b.Bytes()
}

func (s *signer) sign(msg []byte, namespace string) []byte {
	h := sha512.Sum512(msg)
	signed := append([]byte("SSHSIG"), sshString([]byte(namespace), nil, []byte("sha512"), h[:])...)
	sig := ed25519.Sign(s.priv, signed)
	blob := append([]byte("SSHSIG"), 0, 0, 0, 1)
	blob = append(blob, sshString(s.wire, []byte(namespace), nil, []byte("sha512"), sshString([]byte("ssh-ed25519"), sig))...)

	return pem.EncodeToMemory(&pem.Block{Type: "SSH SIGNATURE", Bytes: blob})
}

// server is a fake release host: GitHub's releases/download layout
type server struct {
	t     *testing.T
	key   *signer
	mu    sync.Mutex
	files map[string][]byte
	url   string
}

func newServer(t *testing.T) *server {
	t.Helper()
	s := &server{t: t, key: newSigner(t), files: map[string][]byte{}}
	orig := content.Keys
	content.Keys = func() []string { return []string{s.key.pub} }
	t.Cleanup(func() { content.Keys = orig })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		b, ok := s.files[r.URL.Path]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	s.url = srv.URL

	return s
}

func (s *server) put(path string, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = b
}

// publish packs tree as version and makes it the newest on channel
func (s *server) publish(version, channel string, tree fstest.MapFS) {
	s.t.Helper()
	var bundle bytes.Buffer
	m, err := content.Pack(tree, version, &bundle)
	if err != nil {
		s.t.Fatal(err)
	}
	mb, err := content.MarshalManifest(m)
	if err != nil {
		s.t.Fatal(err)
	}
	dir := "/content-" + version + "/"
	s.put(dir+content.BundleName(version), bundle.Bytes())
	s.put(dir+content.ManifestName, mb)
	s.put(dir+content.ManifestName+".sig", s.key.sign(mb, content.ManifestNamespace))
	s.offer(channel, version)
}

// offer makes channel.json name version as channel's newest
func (s *server) offer(channel, version string) {
	s.t.Helper()
	cb, err := json.Marshal(content.Channel{Channels: map[string]content.Versions{channel: {Content: version}}})
	if err != nil {
		s.t.Fatal(err)
	}
	s.put("/"+ChannelTag+"/"+content.ChannelName, cb)
	s.put("/"+ChannelTag+"/"+content.ChannelName+".sig", s.key.sign(cb, content.ChannelNamespace))
}

func repoFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "content", "env", name))
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// tree is a small content/ with one of each kind and the shipped registry
func tree(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"AGENTS.md":             {Data: []byte("# public instructions\n")},
		"skills/learn/SKILL.md": {Data: []byte("---\nname: learn\n---\n")},
		"skills/plan/SKILL.md":  {Data: []byte("---\nname: plan\n---\n")},
		"agents/review.md":      {Data: []byte("---\ndescription: reviews\n---\nreview it\n")},
		"commands/check.md":     {Data: []byte("run the checks\n")},
		"env/registry.jsonc":    {Data: repoFile(t, "registry.jsonc")},
		"env/base.jsonc":        {Data: repoFile(t, "base.jsonc")},
	}
}

const devProfile = `{"name": "dev", "components": ["ide", "harness"], "channel": "edge"}`

func options(t *testing.T, s *server) Options {
	t.Helper()
	root := t.TempDir()
	p := &paths.Paths{
		Config: filepath.Join(root, "config", "bud"),
		Data:   filepath.Join(root, "data", "bud"),
		State:  filepath.Join(root, "state", "bud"),
		Cache:  filepath.Join(root, "cache", "bud"),
		Home:   root,
	}

	return Options{
		Paths:  p,
		Device: "dev",
		Mirror: s.url,
		Profiles: func() []profile.Dir {
			return []profile.Dir{
				profile.OS(filepath.Join(p.Overlay(), "profiles")),
				{Name: "examples", FS: fstest.MapFS{
					"dev.json":       {Data: []byte(devProfile)},
					"model-box.json": {Data: []byte(`{"name": "model-box", "components": ["qwen"], "channel": "edge"}`)},
				}},
			}
		},
	}
}

func run(t *testing.T, o Options) *Report {
	t.Helper()
	rep, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	return rep
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// snapshot is every regular file under the dirs, by path, links followed
// for the paths OpenCode reads through them
func snapshot(t *testing.T, o Options) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{o.Paths.OpenCodeConfig(), o.Paths.Data, o.Paths.State} {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				out[path] = read(t, path)
			}
			return nil
		})
	}
	for _, p := range []string{"skills/learn/SKILL.md", "skills/plan/SKILL.md"} {
		out["active/"+p] = read(t, filepath.Join(o.Paths.Active(), p))
	}

	return out
}

func TestASyncFromNothingRendersTheConfigAndASecondChangesNothing(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)

	rep := run(t, o)
	if !rep.Changed() || rep.Content != "2026.11.15.1" || rep.From != "" {
		t.Fatalf("first sync: got %+v", rep)
	}
	if got := readLink(o.Paths.ContentCurrent()); got != "2026.11.15.1" {
		t.Fatalf("current links to %q, want the version", got)
	}
	cfg := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc"))
	for _, want := range []string{
		filepath.Join(o.Paths.Active(), "skills"),
		filepath.Join(o.Paths.ContentCurrent(), "AGENTS.md"),
		`"model": "go/glm-5.3"`,
		`"resource": "go test *"`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("opencode.jsonc lacks %s:\n%s", want, cfg)
		}
	}
	if got := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "agents", "review.md")); got != "---\ndescription: reviews\n---\nreview it\n" {
		t.Errorf("agents/review.md = %q", got)
	}
	if got := read(t, filepath.Join(o.Paths.Active(), "skills", "learn", "SKILL.md")); !strings.Contains(got, "name: learn") {
		t.Errorf("the learn skill isn't reachable through active: %q", got)
	}
	if _, err := os.Stat(o.Paths.OpenCodeEnv()); !os.IsNotExist(err) {
		t.Errorf("the public registry has no op:// keys, so there's no env file; stat: %v", err)
	}
	led, err := LoadLedger(o.Paths.SyncLedger())
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range led.Files {
		files = append(files, strings.TrimPrefix(f.Path, o.Paths.OpenCodeConfig()+"/")+":"+f.Kind+":"+f.Layer)
	}
	if want := []string{"agents/review.md:agents:public", "commands/check.md:commands:public", "opencode.jsonc:config:rendered"}; !slices.Equal(files, want) {
		t.Fatalf("ledger files = %v, want %v", files, want)
	}

	before := snapshot(t, o)
	rep = run(t, o)
	if rep.Changed() || len(rep.Skipped) > 0 {
		t.Fatalf("a second sync changed %+v, skipped %+v", rep.Changes, rep.Skipped)
	}
	if after := snapshot(t, o); !maps.Equal(before, after) {
		t.Fatal("a second sync changed files on disk")
	}
}

func TestDeletedConfigAndContentComeBackByteForByte(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	before := snapshot(t, o)

	for _, dir := range []string{o.Paths.OpenCodeConfig(), o.Paths.Content()} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	run(t, o)
	if after := snapshot(t, o); !maps.Equal(before, after) {
		t.Fatalf("got different files back:\nbefore %v\nafter  %v", slices.Sorted(maps.Keys(before)), slices.Sorted(maps.Keys(after)))
	}
}

func TestAHandEditIsSkippedAndNamedWhileTheRestUpdates(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)

	cfg := filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc")
	if err := os.WriteFile(cfg, []byte("// mine\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next := tree(t)
	next["commands/check.md"] = &fstest.MapFile{Data: []byte("run every check\n")}
	next["env/base.jsonc"] = &fstest.MapFile{Data: []byte(`{"permissions": []}`)}
	s.publish("2026.11.15.2", "edge", next)

	rep := run(t, o)
	if len(rep.Skipped) != 1 || rep.Skipped[0].Path != cfg || !strings.Contains(rep.Skipped[0].Why, "changed by hand") {
		t.Fatalf("want opencode.jsonc skipped as a hand edit, got %+v", rep.Skipped)
	}
	if got := read(t, cfg); got != "// mine\n{}\n" {
		t.Fatalf("the hand edit was overwritten: %q", got)
	}
	if got := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "commands", "check.md")); got != "run every check\n" {
		t.Fatalf("commands/check.md wasn't updated: %q", got)
	}
	// it stays in the ledger with bud's hash, so bud doctor can flag it
	led, _ := LoadLedger(o.Paths.SyncLedger())
	if !slices.ContainsFunc(led.Files, func(f File) bool { return f.Path == cfg && f.SHA256 != hash([]byte("// mine\n{}\n")) }) {
		t.Fatalf("the edited file left the ledger: %+v", led.Files)
	}
}

func TestAFileBudDidntWriteIsLeftAloneAndUntracked(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	cfg := filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := run(t, o)
	if len(rep.Skipped) != 1 || !strings.Contains(rep.Skipped[0].Why, "isn't a file bud wrote") {
		t.Fatalf("want the user's opencode.jsonc skipped, got %+v", rep.Skipped)
	}
	if read(t, cfg) != "{}\n" {
		t.Fatal("the user's file was overwritten")
	}
	led, _ := LoadLedger(o.Paths.SyncLedger())
	if slices.ContainsFunc(led.Files, func(f File) bool { return f.Path == cfg }) {
		t.Fatal("a file bud didn't write went into the ledger")
	}
}

func TestDroppedFilesAreDeletedUnlessEdited(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	check := filepath.Join(o.Paths.OpenCodeConfig(), "commands", "check.md")
	if err := os.WriteFile(check, []byte("my own steps\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	next := tree(t)
	delete(next, "agents/review.md")
	delete(next, "commands/check.md")
	s.publish("2026.11.15.2", "edge", next)
	rep := run(t, o)

	if _, err := os.Stat(filepath.Join(o.Paths.OpenCodeConfig(), "agents", "review.md")); !os.IsNotExist(err) {
		t.Fatalf("the dropped agent is still there: %v", err)
	}
	if read(t, check) != "my own steps\n" {
		t.Fatal("an edited file was deleted")
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].Path != check {
		t.Fatalf("want the edited command named, got %+v", rep.Skipped)
	}
	led, _ := LoadLedger(o.Paths.SyncLedger())
	if len(led.Files) != 1 {
		t.Fatalf("want only opencode.jsonc tracked, got %+v", led.Files)
	}
}

func TestADryRunChangesNothing(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	o.DryRun = true

	rep := run(t, o)
	if !rep.Changed() {
		t.Fatal("a dry run from nothing should list changes")
	}
	for _, dir := range []string{o.Paths.Data, o.Paths.State, o.Paths.OpenCodeConfig()} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("a dry run made %s", dir)
		}
	}
}

func TestRollbackGoesBackAndASyncStaysThereUntilANewerVersion(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	v1 := snapshot(t, o)
	next := tree(t)
	next["agents/review.md"] = &fstest.MapFile{Data: []byte("review it harder\n")}
	s.publish("2026.11.15.2", "edge", next)
	run(t, o)

	rep, err := Rollback(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Content != "2026.11.15.1" || readLink(o.Paths.ContentCurrent()) != "2026.11.15.1" {
		t.Fatalf("rollback went to %s", rep.Content)
	}
	agent := filepath.Join(o.Paths.OpenCodeConfig(), "agents", "review.md")
	if got := read(t, agent); got != v1[agent] {
		t.Fatalf("the agent is %q after the rollback, want version 1's", got)
	}

	rep = run(t, o)
	if rep.Changed() || len(rep.Notes) == 0 || !strings.Contains(rep.Notes[0], "rolled back") {
		t.Fatalf("a sync after a rollback should stay put and say so, got %+v", rep)
	}

	s.publish("2026.11.16.1", "edge", tree(t))
	if rep = run(t, o); rep.Content != "2026.11.16.1" {
		t.Fatalf("a newer version should be taken, got %s", rep.Content)
	}
	led, _ := LoadLedger(o.Paths.SyncLedger())
	if len(led.RolledBack) != 0 {
		t.Fatalf("taking a newer version should clear rolled_back, got %v", led.RolledBack)
	}

	if _, err := Rollback(context.Background(), options(t, s)); err == nil || !strings.Contains(err.Error(), "hasn't run") {
		t.Fatalf("rollback with no sync yet: %v", err)
	}
}

func TestAnOlderOfferIsRefused(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	s.publish("2026.11.15.2", "edge", tree(t))
	o := options(t, s)
	run(t, o)

	s.offer("edge", "2026.11.15.1")
	if _, err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "isn't newer") {
		t.Fatalf("want an older offer refused, got %v", err)
	}
}

func TestABadSignatureOrManifestChangesNothing(t *testing.T) {
	t.Run("channel signed by another key", func(t *testing.T) {
		s := newServer(t)
		s.publish("2026.11.15.1", "edge", tree(t))
		other := newSigner(t)
		cb := s.files["/"+ChannelTag+"/channel.json"]
		s.put("/"+ChannelTag+"/channel.json.sig", other.sign(cb, content.ChannelNamespace))
		o := options(t, s)
		if _, err := Run(context.Background(), o); err == nil {
			t.Fatal("a channel.json signed by another key was taken")
		}
		if _, err := os.Stat(o.Paths.Data); !os.IsNotExist(err) {
			t.Fatal("a refused sync wrote to disk")
		}
	})
	t.Run("manifest of another version", func(t *testing.T) {
		s := newServer(t)
		s.publish("2026.11.15.1", "edge", tree(t))
		s.put("/content-2026.11.15.2/content.json", s.files["/content-2026.11.15.1/content.json"])
		s.put("/content-2026.11.15.2/content.json.sig", s.files["/content-2026.11.15.1/content.json.sig"])
		s.offer("edge", "2026.11.15.2")
		if _, err := Run(context.Background(), options(t, s)); err == nil || !strings.Contains(err.Error(), "is for version 2026.11.15.1") {
			t.Fatalf("a manifest for another version was taken: %v", err)
		}
	})
	t.Run("nothing published", func(t *testing.T) {
		s := newServer(t)
		if _, err := Run(context.Background(), options(t, s)); err == nil || !strings.Contains(err.Error(), "no content has been published") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestNoDeviceSaysHowToSetOne(t *testing.T) {
	s := newServer(t)
	o := options(t, s)
	o.Device = ""
	if _, err := Run(context.Background(), o); err != ErrNoDevice {
		t.Fatalf("got %v", err)
	}
}

func TestADeviceWithoutOpenCodeTakesContentOnly(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	o.Device = "model-box"
	rep := run(t, o)
	if _, err := os.Stat(o.Paths.OpenCodeConfig()); !os.IsNotExist(err) {
		t.Fatal("a device that runs no OpenCode got its config")
	}
	if readLink(o.Paths.ContentCurrent()) != "2026.11.15.1" || len(rep.Notes) == 0 {
		t.Fatalf("want content synced with a note, got %+v", rep)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	// These commits are the test's, not the user's: name them, and never
	// sign them, whatever the machine's global commit.gpgsign says.
	flags := []string{
		"-c", "user.name=t", "-c", "user.email=t@example.com",
		"-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false",
	}
	cmd := exec.Command("git", append(flags, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestTheOverlayLayersOverPublicContent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)

	repo := t.TempDir()
	files := map[string]string{
		"overlay.json":                  `{"format": 1}`,
		"profiles/dev.json":             `{"name": "dev", "components": ["harness"], "channel": "edge", "content": {"skills": ["learn"], "env": ["base", "laptop"]}}`,
		"content/AGENTS.md":             "# who you are\n",
		"content/skills/learn/SKILL.md": "---\nname: learn\n---\nmine\n",
		"content/agents/review.md":      "my reviewer\n",
		"env/laptop.jsonc":              `{"theme": "mine"}`,
		"env/registry.jsonc":            `{"version": 1, "providers": {"go": {"api": "openai", "endpoint": "https://opencode.ai/inference/go/openai/v1", "key": "op://vault/item/field"}}}`,
	}
	for name, data := range files {
		path := filepath.Join(repo, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, repo, "init", "-q")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "overlay")
	o.Overlay = repo

	rep := run(t, o)
	if rep.Overlay == "" {
		t.Fatal("the overlay commit wasn't recorded")
	}
	if got := read(t, filepath.Join(o.Paths.Active(), "skills", "learn", "SKILL.md")); !strings.Contains(got, "mine") {
		t.Fatalf("the overlay's learn skill should replace the public one, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(o.Paths.Active(), "skills", "plan")); !os.IsNotExist(err) {
		t.Fatal("the overlay profile takes only learn, but plan is active")
	}
	if got := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "agents", "review.md")); got != "my reviewer\n" {
		t.Fatalf("the overlay's agent should win, got %q", got)
	}
	cfg := read(t, filepath.Join(o.Paths.OpenCodeConfig(), "opencode.jsonc"))
	for _, want := range []string{`"theme": "mine"`, filepath.Join(o.Paths.Overlay(), "content", "AGENTS.md"), "BUD_GO_API_KEY"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("opencode.jsonc lacks %s", want)
		}
	}
	if strings.Contains(cfg, "op://") {
		t.Error("an op:// reference reached opencode.jsonc")
	}
	env := o.Paths.OpenCodeEnv()
	if got := read(t, env); !strings.Contains(got, "BUD_GO_API_KEY=op://vault/item/field\n") {
		t.Fatalf("the env file is %q", got)
	}
	if fi, _ := os.Stat(env); fi.Mode().Perm() != 0o600 {
		t.Fatalf("the env file is %v, want 0600", fi.Mode().Perm())
	}
	led, _ := LoadLedger(o.Paths.SyncLedger())
	if led.Overlay == nil || led.Overlay.Commit != rep.Overlay || led.Overlay.Repo != repo {
		t.Fatalf("ledger overlay = %+v", led.Overlay)
	}
	if !slices.ContainsFunc(led.Files, func(f File) bool { return strings.HasSuffix(f.Path, "agents/review.md") && f.Layer == "overlay" }) {
		t.Fatalf("the agent's layer isn't recorded as overlay: %+v", led.Files)
	}
	if rep := run(t, o); rep.Changed() {
		t.Fatalf("a second sync with the overlay changed %+v", rep.Changes)
	}
}

func TestAFileBudDidntWriteIsNotTakenOverEvenWhenItMatches(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	check := filepath.Join(o.Paths.OpenCodeConfig(), "commands", "check.md")
	if err := os.MkdirAll(filepath.Dir(check), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(check, []byte("run the checks\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, o)
	led, _ := LoadLedger(o.Paths.SyncLedger())
	if slices.ContainsFunc(led.Files, func(f File) bool { return f.Path == check }) {
		t.Fatal("a matching file bud didn't write was taken into the ledger")
	}

	next := tree(t)
	delete(next, "commands/check.md")
	s.publish("2026.11.15.2", "edge", next)
	run(t, o)
	if read(t, check) != "run the checks\n" {
		t.Fatal("a file bud didn't write was deleted")
	}
}

func TestARollbackHoldsUntilAnOfferIsNewerThanEveryRolledBackVersion(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	s.publish("2026.11.15.3", "edge", tree(t))
	run(t, o)
	if _, err := Rollback(context.Background(), o); err != nil {
		t.Fatal(err)
	}

	s.publish("2026.11.15.2", "edge", tree(t))
	if rep := run(t, o); rep.Content != "2026.11.15.1" {
		t.Fatalf("an offer older than the rolled-back version moved the device to %s", rep.Content)
	}
	s.offer("edge", "2026.11.15.3")
	if rep := run(t, o); rep.Content != "2026.11.15.1" {
		t.Fatalf("the rolled-back version came back: %s", rep.Content)
	}
}

func TestASecondSyncWaitsForTheFirst(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	unlock, err := lock(o.Paths.SyncLedger())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "is running on this device") {
		t.Fatalf("a sync ran while another held the lock: %v", err)
	}
	if _, err := Rollback(context.Background(), o); err == nil || !strings.Contains(err.Error(), "is running on this device") {
		t.Fatalf("a rollback ran while a sync held the lock: %v", err)
	}
}

func TestCurrentIsPutBackWhenActiveCantFollow(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)

	// something that isn't a link where active goes: bud won't replace it
	if err := os.Remove(o.Paths.Active()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(o.Paths.Active(), 0o755); err != nil {
		t.Fatal(err)
	}
	s.publish("2026.11.15.2", "edge", tree(t))
	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("the sync should fail when active isn't a link")
	}
	if got := readLink(o.Paths.ContentCurrent()); got != "2026.11.15.1" {
		t.Fatalf("current is %s after a failed switch, want it put back to 2026.11.15.1", got)
	}
}

func TestCurrentIsRemovedWhenTheFirstActiveSwitchFails(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	if err := os.MkdirAll(o.Paths.Active(), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), o); err == nil {
		t.Fatal("the sync should fail when active isn't a link")
	}
	if got := readLink(o.Paths.ContentCurrent()); got != "" {
		t.Fatalf("current points to %s after a failed first switch", got)
	}
	if _, err := os.Lstat(o.Paths.ContentCurrent()); !os.IsNotExist(err) {
		t.Fatalf("current still exists after the failed first switch: %v", err)
	}
}

func TestLinkSetBuildRejectsMissingTargets(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "active")
	target := filepath.Join(root, "missing")

	if err := (linkSet{"skills/learn": target}).build(active); err == nil {
		t.Fatal("a link set with a missing target was built")
	}
	if _, err := os.Lstat(filepath.Join(root, activeDirs)); !os.IsNotExist(err) {
		t.Fatalf("active links were created for an invalid target: %v", err)
	}
}

func TestContentOnDiskNeedingANewerFormatIsRefused(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)
	run(t, o)
	s.publish("2026.11.15.2", "edge", tree(t))
	run(t, o)
	// as a newer bud would have saved it, before this one replaced it
	saved := filepath.Join(o.Paths.Content(), "2026.11.15.1", content.ManifestName)
	if err := os.WriteFile(saved, []byte(fmt.Sprintf(`{"format": %d, "version": "2026.11.15.1", "files": []}`, content.Format+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, o)

	if _, err := Rollback(context.Background(), o); err == nil || !strings.Contains(err.Error(), "run bud update") {
		t.Fatalf("rolling back to it: %v", err)
	}
	if !maps.Equal(snapshot(t, o), before) {
		t.Fatal("a refused rollback changed files")
	}

	saved = filepath.Join(o.Paths.Content(), "2026.11.15.2", content.ManifestName)
	if err := os.WriteFile(saved, []byte(fmt.Sprintf(`{"format": %d, "version": "2026.11.15.2", "files": []}`, content.Format+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "run bud update") {
		t.Fatalf("syncing the version on disk: %v", err)
	}
}

// codingOptions is a device whose profile takes the coding fragment, the one
// that adds the cocoindex server
func codingOptions(t *testing.T, s *server) Options {
	t.Helper()
	o := options(t, s)
	o.Profiles = func() []profile.Dir {
		return []profile.Dir{{Name: "examples", FS: fstest.MapFS{
			"dev.json": {Data: []byte(`{"name": "dev", "components": ["ide", "harness"], "channel": "edge", "content": {"env": ["base", "coding"]}}`)},
		}}}
	}

	return o
}

func codingTree(t *testing.T) fstest.MapFS {
	t.Helper()
	tr := tree(t)
	tr["env/coding.jsonc"] = &fstest.MapFile{Data: repoFile(t, "coding.jsonc")}

	return tr
}

func TestADeviceWithCocoindexGetsItsSettingsFromTheRegistry(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", codingTree(t))
	o := codingOptions(t, s)

	run(t, o)
	got := read(t, o.Paths.CocoindexSettings())
	for _, want := range []string{"provider: litellm", `model: "openai/qwen3-embedding-0.6b"`, "OPENAI_API_BASE: \"http://home.invalid:8082/v1\"", "OPENAI_API_KEY: not-needed"} {
		if !strings.Contains(got, want) {
			t.Errorf("global_settings.yml lacks %s:\n%s", want, got)
		}
	}
	led, _ := LoadLedger(o.Paths.SyncLedger())
	if !slices.ContainsFunc(led.Files, func(f File) bool { return f.Path == o.Paths.CocoindexSettings() && f.Kind == "config" }) {
		t.Fatalf("the settings aren't in the ledger: %+v", led.Files)
	}
	if rep := run(t, o); rep.Changed() || len(rep.Skipped) > 0 {
		t.Fatalf("a second sync changed %+v, skipped %+v", rep.Changes, rep.Skipped)
	}

	// a hand edit stays, and the sync names it
	if err := os.WriteFile(o.Paths.CocoindexSettings(), []byte("embedding: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := run(t, o)
	if len(rep.Skipped) != 1 || rep.Skipped[0].Path != o.Paths.CocoindexSettings() || !strings.Contains(rep.Skipped[0].Why, "changed by hand") {
		t.Fatalf("want the settings skipped as a hand edit, got %+v", rep.Skipped)
	}
	if got := read(t, o.Paths.CocoindexSettings()); got != "embedding: {}\n" {
		t.Fatalf("the hand edit was overwritten: %q", got)
	}
}

func TestADeviceWithoutCocoindexGetsNoSettings(t *testing.T) {
	s := newServer(t)
	s.publish("2026.11.15.1", "edge", tree(t))
	o := options(t, s)

	run(t, o)
	if _, err := os.Stat(o.Paths.CocoindexSettings()); !os.IsNotExist(err) {
		t.Fatalf("a device without the cocoindex server got settings; stat: %v", err)
	}
}
