package cli

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"jgarner.dev/bud/internal/atomicfile"
	"jgarner.dev/bud/internal/content"
)

func init() {
	RegisterCommand(Content())
}

// Content builds content bundles (Suite D60). CI runs it on a push to main
// that changes content/; a device never needs it
func Content() *Command {
	var version, out, from, set string
	var fresh bool
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
			{
				Name:  "channel",
				Usage: "move one channel's content version in channel.json: channel (--from channel.json | --new) --set <channel>=<version> [--out dir]",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&from, "from", "", "the published channel.json to start from; its signature, <file>.sig, must verify")
					fs.BoolVar(&fresh, "new", false, "start from an empty channel.json (the first content release only)")
					fs.StringVar(&set, "set", "", "the channel and version to move it to, e.g. edge=2026.11.15.1")
					fs.StringVar(&out, "out", ".", "where to write channel.json")
				},
				Run: func(_ *Globals, args []string) error { return contentChannel(args, from, fresh, set, out) },
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

// contentChannel is what the content release workflow runs to move edge to
// a new release, or stable to one edge has had. It starts from the
// published channel.json only once its signature verifies, so a tampered
// file is never re-signed, and needs --new to start from nothing, so a
// download that failed can't quietly wipe the other channel
func contentChannel(args []string, from string, fresh bool, set, out string) error {
	if len(args) != 0 {
		return usageErrorf("content channel takes no arguments, e.g. bud content channel --from channel.json --set edge=2026.11.15.1")
	}
	name, version, ok := strings.Cut(set, "=")
	if !ok || name == "" || version == "" {
		return usageErrorf("content channel needs --set <channel>=<version>, e.g. --set edge=2026.11.15.1")
	}
	if (from == "") == !fresh {
		return usageErrorf("content channel needs one of --from channel.json (to update the published one) or --new (the first release)")
	}

	c := &content.Channel{}
	if from != "" {
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(from + ".sig")
		if err != nil {
			return fmt.Errorf("read the published channel.json's signature: %w", err)
		}
		if c, err = content.VerifyChannel(b, sig); err != nil {
			return err
		}
	}
	before := c.Channels[name].Content
	changed, err := content.SetContent(c, name, version)
	if err != nil {
		return err
	}
	b, err := content.MarshalChannel(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	path := filepath.Join(out, content.ChannelName)
	if err := atomicfile.WriteFile(path, b, 0o644); err != nil {
		return err
	}

	if !changed {
		fmt.Printf("%s is already at %s; wrote %s unchanged\n", name, version, path)
		return nil
	}
	if before == "" {
		before = "nothing"
	}
	fmt.Printf("%s: %s -> %s in %s; sign it next\n", name, before, version, path)
	return nil
}
