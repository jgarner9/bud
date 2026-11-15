package content

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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

// trust makes s the only key content trusts for this test
func trust(t *testing.T, s *signer) {
	t.Helper()
	orig := Keys
	Keys = func() []string { return []string{s.pub} }
	t.Cleanup(func() { Keys = orig })
}

var tree = fstest.MapFS{
	"AGENTS.md":              {Data: []byte("# global\n")},
	"README.md":              {Data: []byte("repo note, never ships\n")},
	"skills/learn/SKILL.md":  {Data: []byte("---\nname: learn\n---\n")},
	"env/registry.jsonc":     {Data: []byte("{}\n")},
	"commands/check.md":      {Data: []byte("run the checks\n")},
	"skills/learn/notes.txt": {Data: []byte("extra\n")},
}

func pack(t *testing.T) (*Manifest, []byte) {
	t.Helper()
	var buf bytes.Buffer
	m, err := Pack(tree, "2026.11.15.1", &buf)
	if err != nil {
		t.Fatal(err)
	}

	return m, buf.Bytes()
}

func TestPackListsEveryFileSortedWithItsKind(t *testing.T) {
	m, _ := pack(t)
	var got []string
	for _, f := range m.Files {
		got = append(got, f.Path+" "+f.Kind)
	}
	want := "AGENTS.md instructions,commands/check.md commands,env/registry.jsonc env,skills/learn/SKILL.md skills,skills/learn/notes.txt skills"
	if strings.Join(got, ",") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, ","), want)
	}
}

func TestPackIsDeterministic(t *testing.T) {
	_, a := pack(t)
	_, b := pack(t)
	if !bytes.Equal(a, b) {
		t.Fatal("the same tree packed to different bytes")
	}
}

func TestPackRefusesWhatContentDoesntHold(t *testing.T) {
	bad := fstest.MapFS{"notes/todo.md": {Data: []byte("x")}}
	if _, err := Pack(bad, "2026.11.15.1", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "content/notes") {
		t.Fatalf("got %v", err)
	}
	link := fstest.MapFS{"skills/x": {Mode: os.ModeSymlink, Data: []byte("/etc")}}
	if _, err := Pack(link, "2026.11.15.1", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "regular files") {
		t.Fatalf("got %v", err)
	}
	slash := fstest.MapFS{`skills/learn/notes\draft.md`: {Data: []byte("x")}}
	if _, err := Pack(slash, "2026.11.15.1", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "backslashes") {
		t.Fatalf("a path Unpack refuses should fail at pack time, got %v", err)
	}
	if _, err := Pack(tree, "1.2.3", &bytes.Buffer{}); err == nil {
		t.Fatal("want an error for a code-style version")
	}
}

func TestASignedBundleUnpacksAndVerifies(t *testing.T) {
	s := newSigner(t)
	trust(t, s)
	m, bundle := pack(t)
	mb, _ := MarshalManifest(m)

	verified, err := VerifyManifest(mb, s.sign(mb, ManifestNamespace))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "content", verified.Version)
	if err := Unpack(bytes.NewReader(bundle), verified, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "skills", "learn", "SKILL.md")); string(b) != "---\nname: learn\n---\n" {
		t.Fatalf("SKILL.md = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, ManifestName)); !bytes.Equal(b, mb) {
		t.Fatal("the version dir should keep its content.json")
	}
	if _, err := os.Stat(filepath.Join(dst, "README.md")); err == nil {
		t.Fatal("README.md shipped")
	}
	if err := Unpack(bytes.NewReader(bundle), verified, dst); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unpacking over a version: %v", err)
	}
}

func TestSignaturesAreRefusedWhenWrong(t *testing.T) {
	s := newSigner(t)
	trust(t, s)
	m, _ := pack(t)
	mb, _ := MarshalManifest(m)
	cb := []byte(`{"channels": {"edge": {"content": "2026.11.15.1"}}}`)

	for name, tc := range map[string]struct {
		verify func() error
		want   string
	}{
		"another key": {func() error {
			_, err := VerifyManifest(mb, newSigner(t).sign(mb, ManifestNamespace))
			return err
		}, "different key"},
		"a release signature on a manifest": {func() error {
			_, err := VerifyManifest(mb, s.sign(mb, "bud-release"))
			return err
		}, "namespace"},
		"a channel signature on a manifest": {func() error {
			_, err := VerifyManifest(mb, s.sign(mb, ChannelNamespace))
			return err
		}, "namespace"},
		"a changed manifest": {func() error {
			_, err := VerifyManifest(append([]byte(" "), mb...), s.sign(mb, ManifestNamespace))
			return err
		}, "doesn't match"},
		"a manifest signature on a channel": {func() error {
			_, err := VerifyChannel(cb, s.sign(cb, ManifestNamespace))
			return err
		}, "namespace"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := tc.verify(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}

	c, err := VerifyChannel(cb, s.sign(cb, ChannelNamespace))
	if err != nil || c.Channels["edge"].Content != "2026.11.15.1" {
		t.Fatalf("a good channel.json: %+v, %v", c, err)
	}
}

func TestNoTrustedKeyMeansNothingVerifies(t *testing.T) {
	orig := Keys
	Keys = func() []string { return nil }
	t.Cleanup(func() { Keys = orig })
	if _, err := VerifyManifest([]byte("{}"), nil); err == nil || !strings.Contains(err.Error(), "no release key") {
		t.Fatalf("got %v", err)
	}
}

// rawBundle builds a bundle by hand, for the shapes Pack never makes
func rawBundle(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		if h.Typeflag == 0 {
			h.Typeflag = tar.TypeReg
		}
		tw.WriteHeader(h)
		if h.Typeflag == tar.TypeReg {
			tw.Write(bytes.Repeat([]byte("x"), int(h.Size)))
		}
	}
	tw.Close()
	gz.Close()

	return buf.Bytes()
}

// appendedArchive is one gzip member holding a valid one-file tar, then a
// second tar right after its end blocks, small enough to pass as padding
func appendedArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	for _, name := range []string{"skills/a.md", "skills/evil.md"} {
		tw := tar.NewWriter(gz)
		tw.WriteHeader(&tar.Header{Name: name, Size: 1, Typeflag: tar.TypeReg})
		tw.Write([]byte("x"))
		tw.Close()
	}
	gz.Close()

	return buf.Bytes()
}

// corrupt changes a copy of b, leaving b as it is
func corrupt(b []byte, change func([]byte) []byte) []byte {
	return change(append([]byte(nil), b...))
}

func TestUnpackRefusesAnythingTheManifestDoesntVouchFor(t *testing.T) {
	m, bundle := pack(t)
	// "x" hashed, so a one-byte "x" entry matches its manifest line
	xFile := File{Path: "skills/a.md", Kind: "skills", Size: 1, SHA256: "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"}
	one := &Manifest{Version: "2026.11.15.1", Files: []File{xFile}}

	for name, tc := range map[string]struct {
		m      *Manifest
		bundle []byte
		want   string
	}{
		"an unlisted file":   {one, rawBundle(t, &tar.Header{Name: "skills/a.md", Size: 1}, &tar.Header{Name: "skills/b.md", Size: 1}), "doesn't list"},
		"a missing file":     {&Manifest{Version: "2026.11.15.1", Files: append([]File{xFile}, File{Path: "skills/c.md", Size: 1})}, rawBundle(t, &tar.Header{Name: "skills/a.md", Size: 1}), "missing from the bundle"},
		"a changed file":     {&Manifest{Version: m.Version, Files: append([]File(nil), m.Files...)}, bundle, "changed after it was signed"},
		"a wrong size":       {one, rawBundle(t, &tar.Header{Name: "skills/a.md", Size: 2}), "2 bytes"},
		"a parent path":      {one, rawBundle(t, &tar.Header{Name: "../a.md", Size: 1}), "plain relative path"},
		"an absolute path":   {one, rawBundle(t, &tar.Header{Name: "/etc/a.md", Size: 1}), "plain relative path"},
		"a symlink":          {one, rawBundle(t, &tar.Header{Name: "skills/a.md", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}), "isn't a regular file"},
		"a duplicate":        {one, rawBundle(t, &tar.Header{Name: "skills/a.md", Size: 1}, &tar.Header{Name: "skills/a.md", Size: 1}), "twice"},
		"a manifest escape":  {&Manifest{Version: "2026.11.15.1", Files: []File{{Path: "../../x"}}}, rawBundle(t), "isn't a plain relative path"},
		"the manifest's own": {&Manifest{Version: "2026.11.15.1", Files: []File{{Path: ManifestName}}}, rawBundle(t), "isn't a plain relative path"},
		"a listed twice":     {&Manifest{Version: "2026.11.15.1", Files: []File{xFile, {Path: xFile.Path, Size: 2, SHA256: xFile.SHA256}}}, rawBundle(t, &tar.Header{Name: "skills/a.md", Size: 1}), "twice"},
		"a corrupt trailer":  {m, corrupt(bundle, func(b []byte) []byte { b[len(b)-8] ^= 0xff; return b }), "read the bundle"},
		"a cut-off trailer":  {m, corrupt(bundle, func(b []byte) []byte { return b[:len(b)-4] }), "read the bundle"},
		"a second member":    {m, append(append([]byte(nil), bundle...), bundle...), "data after"},
		"a second archive":   {one, appendedArchive(t), "data after"},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "a changed file" {
				tc.m.Files[0].SHA256 = strings.Repeat("0", 64)
			}
			parent := t.TempDir()
			dst := filepath.Join(parent, "v")
			err := Unpack(bytes.NewReader(tc.bundle), tc.m, dst)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if entries, _ := os.ReadDir(parent); len(entries) != 0 {
				t.Fatalf("a refused bundle left %v behind", entries)
			}
		})
	}
}

func TestVersions(t *testing.T) {
	for _, bad := range []string{"", "1.2.3", "2026.13.01.1", "2026.11.15.0", "2026.11.15.01", "2026.1.15.1", "2026.02.30.1", "2026.11.15.x"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q): want an error", bad)
		}
	}
	a, _ := ParseVersion("2026.11.15.2")
	b, _ := ParseVersion("2026.11.15.10")
	c, _ := ParseVersion("2026.12.01.1")
	if a.Compare(b) != -1 || c.Compare(b) != 1 || a.Compare(a) != 0 {
		t.Fatal("versions compare numerically, part by part")
	}
}

func TestOnlyANewerVersionIsTaken(t *testing.T) {
	if err := CheckNewer("", "2026.11.15.1"); err != nil {
		t.Fatalf("nothing installed: %v", err)
	}
	if err := CheckNewer("2026.11.15.1", "2026.11.15.2"); err != nil {
		t.Fatalf("newer: %v", err)
	}
	for _, offered := range []string{"2026.11.15.1", "2026.11.14.9"} {
		err := CheckNewer("2026.11.15.1", offered)
		if err == nil || !strings.Contains(err.Error(), "--rollback") {
			t.Errorf("offered %s over 2026.11.15.1: %v", offered, err)
		}
	}
}

func TestANewerContentFormatIsRefused(t *testing.T) {
	s := newSigner(t)
	trust(t, s)
	m, _ := pack(t)
	if mb, _ := MarshalManifest(m); bytes.Contains(mb, []byte(`"format"`)) {
		t.Fatalf("format 1 should leave the field out, so older buds read it: %s", mb)
	}

	for _, tc := range []struct {
		format int
		ok     bool
	}{{0, true}, {Format, true}, {Format + 1, false}} {
		m.Format = tc.format
		mb, _ := MarshalManifest(m)
		_, err := VerifyManifest(mb, s.sign(mb, ManifestNamespace))
		if tc.ok && err != nil {
			t.Errorf("format %d: %v", tc.format, err)
		}
		if !tc.ok && (err == nil || !strings.Contains(err.Error(), "run bud update")) {
			t.Errorf("format %d: %v, want a refusal naming bud update", tc.format, err)
		}
	}
}
