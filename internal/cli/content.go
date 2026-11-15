package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/content"
)

func init() {
	RegisterCommand(Content())
}

// Content builds content bundles (Suite D60). CI runs it on a push to main
// that changes content/; a device never needs it
func Content() *Command {
	var version, out string
	return &Command{
		Name:  "content",
		Usage: "Builds the signed content bundle CI publishes",
		Subs: []*Command{
			{
				Name:  "pack",
				Usage: "pack a content dir into content-<version>.tar.gz and content.json: pack --version V [--out dir] <content dir>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&version, "version", "", "the content version, YYYY.MM.DD.N")
					fs.StringVar(&out, "out", ".", "where to write the bundle and content.json")
				},
				Run: func(_ *Globals, args []string) error { return contentPack(args, version, out) },
			},
		},
	}
}

func contentPack(args []string, version, out string) error {
	if len(args) != 1 {
		return usageErrorf("content pack takes the content dir, e.g. bud content pack --version 2026.11.15.1 content")
	}
	if version == "" {
		return usageErrorf("content pack needs --version YYYY.MM.DD.N")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var bundle bytes.Buffer
	m, err := content.Pack(os.DirFS(args[0]), version, &bundle)
	if err != nil {
		return err
	}
	mb, err := content.MarshalManifest(m)
	if err != nil {
		return err
	}
	bundlePath := filepath.Join(out, content.BundleName(version))
	if err := atomicfile.WriteFile(bundlePath, bundle.Bytes(), 0o644); err != nil {
		return err
	}
	if err := atomicfile.WriteFile(filepath.Join(out, content.ManifestName), mb, 0o644); err != nil {
		return err
	}

	fmt.Printf("packed %d files into %s; sign %s next\n", len(m.Files), bundlePath, filepath.Join(out, content.ManifestName))
	return nil
}
