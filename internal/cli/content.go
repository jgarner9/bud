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
	var version, out, edge, stable, channel string
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
				Usage: "write channel.json with each channel's content version: channel --edge V [--stable V] [--out dir]",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&edge, "edge", "", "the content version edge offers, YYYY.MM.DD.N")
					fs.StringVar(&stable, "stable", "", "the content version stable offers; leave out until one is promoted")
					fs.StringVar(&out, "out", ".", "where to write channel.json")
				},
				Run: func(_ *Globals, args []string) error { return contentChannel(args, edge, stable, out) },
			},
			{
				Name:  "offered",
				Usage: "print the content version a published channel.json offers on one channel, once its signature verifies: offered --channel edge <channel.json>",
				Flags: func(fs *flag.FlagSet) {
					fs.StringVar(&channel, "channel", "edge", "the channel to read")
				},
				Run: func(_ *Globals, args []string) error { return contentOffered(args, channel) },
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

// contentChannel writes channel.json for the content release workflow,
// which works out both versions from the release tags on every run
func contentChannel(args []string, edge, stable, out string) error {
	if len(args) != 0 {
		return usageErrorf("content channel takes no arguments, e.g. bud content channel --edge 2026.11.15.2 --stable 2026.11.15.1")
	}
	if edge == "" {
		return usageErrorf("content channel needs --edge YYYY.MM.DD.N")
	}
	c, err := content.NewChannel(edge, stable)
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

	if stable == "" {
		stable = "nothing yet"
	}
	fmt.Printf("wrote %s: edge %s, stable %s; sign it next\n", path, edge, stable)
	return nil
}

// contentOffered prints what a published channel.json offers on one
// channel, refusing it unless <file>.sig verifies. Promoting reads it, so
// stable only ever takes the version edge devices were offered
func contentOffered(args []string, channel string) error {
	if len(args) != 1 {
		return usageErrorf("content offered takes the channel.json, e.g. bud content offered --channel edge channel.json")
	}
	b, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(args[0] + ".sig")
	if err != nil {
		return fmt.Errorf("read the channel.json's signature: %w", err)
	}
	c, err := content.VerifyChannel(b, sig)
	if err != nil {
		return err
	}
	v, ok := c.Channels[channel]
	if !ok || v.Content == "" {
		return fmt.Errorf("channel.json offers no content on %s", channel)
	}

	fmt.Println(v.Content)
	return nil
}
