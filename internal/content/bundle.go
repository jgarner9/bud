package content

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// BundleName is the archive name for a content version
func BundleName(version string) string { return "content-" + version + ".tar.gz" }

// Pack writes src (the repo's content/ dir) as a bundle to w and returns its
// manifest. The archive is deterministic: files sorted, and no times, owners
// or modes beyond 0644, so the same tree always packs to the same bytes
func Pack(src fs.FS, version string, w io.Writer) (*Manifest, error) {
	if _, err := ParseVersion(version); err != nil {
		return nil, err
	}
	m := &Manifest{Version: version}
	if Format > 1 {
		m.Format = Format
	}
	var files [][]byte
	err := fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return err
		}
		top, _, _ := strings.Cut(p, "/")
		if slices.Contains(skipped, top) {
			return nil
		}
		kind, ok := Kinds[top]
		if !ok {
			return fmt.Errorf("content/%s: not something content/ holds (%s); move it or add it to content.Kinds", p, strings.Join(kindNames(), ", "))
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("content/%s: only regular files ship, not links or devices", p)
		}
		if !safePath(p) {
			// Unpack refuses it, so a bundle with it could be signed but never installed
			return fmt.Errorf("content/%s: devices can't install this path (no backslashes); rename it", p)
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		m.Files = append(m.Files, File{Path: p, Kind: kind, Size: int64(len(b)), SHA256: hex.EncodeToString(sum[:])})
		files = append(files, b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(m.Files) == 0 {
		return nil, errors.New("content/ has nothing to ship")
	}

	// WalkDir goes in lexical order, so Files is sorted already
	gz, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	tw := tar.NewWriter(gz)
	for i, f := range m.Files {
		hdr := &tar.Header{Name: f.Path, Mode: 0o644, Size: f.Size, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(files[i]); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}

	return m, nil
}

func kindNames() []string {
	var names []string
	for k := range Kinds {
		names = append(names, k)
	}
	slices.Sort(names)

	return names
}

// MarshalManifest is content.json's bytes, the ones that get signed
func MarshalManifest(m *Manifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}

// Unpack extracts a bundle into dst, which must not exist yet, checking it
// against m (already verified): every entry must be a regular file the
// manifest lists, with its size and hash, and every listed file must be
// there. It extracts into a temp dir beside dst and renames it into place,
// so dst is either the whole verified version or absent. content.json is
// written into dst too, as the record of what it holds
func Unpack(r io.Reader, m *Manifest, dst string) (err error) {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists; content versions are never unpacked over each other", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmp)
		}
	}()

	want := make(map[string]File, len(m.Files))
	for _, f := range m.Files {
		if !safePath(f.Path) {
			return fmt.Errorf("content.json lists %q, which isn't a plain relative path", f.Path)
		}
		if _, dup := want[f.Path]; dup {
			return fmt.Errorf("content.json lists %q twice", f.Path)
		}
		want[f.Path] = f
	}

	// gzip reads a ByteReader without buffering past its member, so br shows
	// whether anything follows it
	br := bufio.NewReader(r)
	gz, err := gzip.NewReader(br)
	if err != nil {
		return fmt.Errorf("read the bundle: %w", err)
	}
	// one gzip member only; anything after it is refused below
	gz.Multistream(false)
	tr := tar.NewReader(gz)
	got := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read the bundle: %w", err)
		}
		f, ok := want[hdr.Name]
		switch {
		case hdr.Typeflag != tar.TypeReg:
			return fmt.Errorf("the bundle holds %q, which isn't a regular file", hdr.Name)
		case !safePath(hdr.Name):
			return fmt.Errorf("the bundle holds %q, which isn't a plain relative path", hdr.Name)
		case !ok:
			return fmt.Errorf("the bundle holds %q, which content.json doesn't list", hdr.Name)
		case got[hdr.Name]:
			return fmt.Errorf("the bundle holds %q twice", hdr.Name)
		case hdr.Size != f.Size:
			return fmt.Errorf("%s: the bundle's copy is %d bytes, content.json says %d", hdr.Name, hdr.Size, f.Size)
		}
		got[hdr.Name] = true
		if err := writeChecked(tmp, f, tr); err != nil {
			return err
		}
	}
	// tar stops at its end blocks, before gzip checks its trailer (CRC and
	// length), so read what's left. A little zero padding is normal; anything
	// else, such as a second archive, never went through the checks above
	rest, err := io.ReadAll(io.LimitReader(gz, maxTrailing+1))
	if err != nil {
		return fmt.Errorf("read the bundle: %w", err)
	}
	if len(rest) > maxTrailing || slices.ContainsFunc(rest, func(b byte) bool { return b != 0 }) {
		return errors.New("read the bundle: data after the archive's end")
	}
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		return errors.New("read the bundle: data after the archive's end")
	}
	for _, f := range m.Files {
		if !got[f.Path] {
			return fmt.Errorf("%s: listed in content.json but missing from the bundle", f.Path)
		}
	}

	b, err := MarshalManifest(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, ManifestName), b, 0o644); err != nil {
		return err
	}

	return os.Rename(tmp, dst)
}

// maxTrailing is how much padding may follow tar's end blocks inside the
// gzip stream; tar pads to its 10 KiB record size at most
const maxTrailing = 10 << 10

func writeChecked(root string, f File, r io.Reader) error {
	full := filepath.Join(root, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), io.LimitReader(r, f.Size))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", f.Path, err)
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != f.SHA256 {
		return fmt.Errorf("%s: its sha256 is %s, content.json says %s; the bundle was changed after it was signed", f.Path, sum, f.SHA256)
	}

	return nil
}

// safePath is a relative, slash-separated path with no . or .. parts and no
// leading slash, so it can't land outside the version's dir; the manifest
// name is reserved for the manifest itself
func safePath(p string) bool {
	return p != "" && p != ManifestName && fs.ValidPath(p) && path.Clean(p) == p && !strings.Contains(p, "\\")
}
