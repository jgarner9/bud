// Package content builds and checks content bundles (Suite D57, D60, D61):
// the signed archive of content/ that reaches every device running OpenCode.
//
// CI packs content/ into content-<version>.tar.gz plus content.json, a
// manifest listing each file's path, kind, size and sha256, and signs the
// manifest. A device verifies the manifest's signature against the release
// key, then every file it extracts against the manifest, so a file that
// isn't listed, or doesn't match, never lands. channel.json names the newest
// content and code version on each channel and is signed the same way. Each
// is signed under a namespace of its own, so no signature made for one can
// pass as another or as a code release's
package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"jgarner.dev/bud/internal/updater"
)

const (
	// ManifestNamespace is the ssh-keygen -n namespace content.json is signed under
	ManifestNamespace = "bud-content"
	// ChannelNamespace is the namespace channel.json is signed under
	ChannelNamespace = "bud-channel"

	ManifestName = "content.json"
	ChannelName  = "channel.json"
)

// Kinds maps each top-level entry of content/ to its kind. Anything else
// there is an error at pack time, so a stray file is noticed rather than
// shipped; README.md is the repo's own note and is left out
var Kinds = map[string]string{
	"AGENTS.md": "instructions",
	"skills":    "skills",
	"agents":    "agents",
	"commands":  "commands",
	"plugins":   "plugins",
	"env":       "env",
}

// FormatName is the file in content/ that says which content format the
// tree needs; it never ships, and without it the tree is format 1
const FormatName = "FORMAT"

// skipped are top-level entries of content/ that never ship
var skipped = []string{"README.md", FormatName}

// Format is the newest content format this bud reads. When content starts
// using something an older bud can't read, such as a new registry field,
// raise this and write the new number to content/FORMAT in the same change,
// so an older bud refuses the bundle with a message to update rather than
// failing on the file it can't parse. The format comes from the tree, not
// from this constant, so re-packing an old release keeps the format it was
// published with. Format 1 is written as no field at all, which keeps
// manifests readable by every bud that predates it. Format 2 (JGA-358): env
// fragments carry the bud block (MCP servers and the gateway), which an
// older bud would pass into opencode.jsonc as it is
const Format = 2

// Manifest is content.json
type Manifest struct {
	// Format is the content format the bundle needs; 0 means 1
	Format  int    `json:"format,omitempty"`
	Version string `json:"version"`
	Files   []File `json:"files"`
}

type File struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Channel is channel.json: the newest versions on each channel (Suite D61)
type Channel struct {
	Channels map[string]Versions `json:"channels"`
}

type Versions struct {
	Content string `json:"content,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Keys are the public keys bundles must be signed by; the release keys,
// since one key signs everything Bud ships (namespaces keep them apart)
var Keys = func() []string { return updater.ReleaseKeys }

// VerifyManifest checks content.json's signature, then parses it
func VerifyManifest(manifest, sig []byte) (*Manifest, error) {
	if err := verify(manifest, sig, ManifestNamespace); err != nil {
		return nil, fmt.Errorf("content.json: %w", err)
	}
	var m Manifest
	if err := decodeStrict(manifest, &m); err != nil {
		return nil, fmt.Errorf("content.json: %w", err)
	}
	if _, err := ParseVersion(m.Version); err != nil {
		return nil, fmt.Errorf("content.json: %w", err)
	}
	if err := CheckFormat(m.Version, m.Format); err != nil {
		return nil, err
	}

	return &m, nil
}

// CheckFormat refuses content version that needs a newer format than this
// bud reads
func CheckFormat(version string, format int) error {
	if format > Format {
		return fmt.Errorf("content %s needs content format %d, but this bud reads up to %d: run bud update (on edge, the next nightly has it), then bud sync; nothing changed", version, format, Format)
	}

	return nil
}

// CheckSaved checks the content.json that Unpack saved in a version dir,
// so content already on disk (after a bud downgrade, say) is held to the
// same format as a download. It was verified when it was unpacked, so it
// isn't checked again here, and fields this bud doesn't know are ignored
func CheckSaved(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return fmt.Errorf("%w; remove %s so bud sync downloads it again", err, dir)
	}
	var m struct {
		Format  int    `json:"format"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("%s: %w; remove %s so bud sync downloads it again", filepath.Join(dir, ManifestName), err, dir)
	}

	return CheckFormat(m.Version, m.Format)
}

// VerifyChannel checks channel.json's signature, then parses it
func VerifyChannel(channel, sig []byte) (*Channel, error) {
	if err := verify(channel, sig, ChannelNamespace); err != nil {
		return nil, fmt.Errorf("channel.json: %w", err)
	}
	var c Channel
	if err := decodeStrict(channel, &c); err != nil {
		return nil, fmt.Errorf("channel.json: %w", err)
	}

	return &c, nil
}

func verify(msg, sig []byte, namespace string) error {
	keys := Keys()
	if len(keys) == 0 {
		return updater.ErrNoReleaseKey
	}
	var errs []error
	for _, key := range keys {
		err := updater.VerifySSHSig(key, namespace, msg, sig)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("there is more after the closing }")
	}

	return nil
}

// Version is a content version, YYYY.MM.DD.N: the day CI cut it and which
// cut of that day it is, so versions sort by when they were made
type Version struct {
	Year, Month, Day, N int
}

// ParseVersion reads a content version
func ParseVersion(s string) (Version, error) {
	parts := strings.Split(s, ".")
	bad := fmt.Errorf("%q isn't a content version (YYYY.MM.DD.N)", s)
	if len(parts) != 4 {
		return Version{}, bad
	}
	var n [4]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}, bad
		}
		n[i] = v
	}
	v := Version{n[0], n[1], n[2], n[3]}
	// one spelling per version, so two strings never name the same one
	if _, err := time.Parse("2006.01.02", fmt.Sprintf("%04d.%02d.%02d", v.Year, v.Month, v.Day)); err != nil || v.N < 1 || v.String() != s {
		return Version{}, bad
	}

	return v, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%04d.%02d.%02d.%d", v.Year, v.Month, v.Day, v.N)
}

// Compare returns -1, 0 or 1 as v is older than, the same as, or newer than o
func (v Version) Compare(o Version) int {
	return slices.Compare([]int{v.Year, v.Month, v.Day, v.N}, []int{o.Year, o.Month, o.Day, o.N})
}

// CheckNewer refuses offered unless it is newer than installed (Suite D60),
// so an old signed bundle can't be replayed to roll a device back. Going
// back is bud sync --rollback, to a version still on disk. An empty
// installed means nothing is installed yet
func CheckNewer(installed, offered string) error {
	o, err := ParseVersion(offered)
	if err != nil {
		return err
	}
	if installed == "" {
		return nil
	}
	i, err := ParseVersion(installed)
	if err != nil {
		return fmt.Errorf("the installed content version: %w", err)
	}
	if o.Compare(i) <= 0 {
		return fmt.Errorf("refusing content %s: it isn't newer than the installed %s (to go back, use bud sync --rollback)", offered, installed)
	}

	return nil
}
