package syncer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/updater"
)

// ChannelTag is the release that holds channel.json and its signature; each
// content version is the release content-<version> beside it. A mirror
// serves the same paths under its own base URL
const ChannelTag = "content-channel"

// DefaultBase is where content releases come from without a mirror
var DefaultBase = "https://github.com/" + updater.Repo + "/releases/download"

var client = &http.Client{Timeout: 5 * time.Minute}

const (
	maxJSON   = 4 << 20   // content.json, channel.json and their signatures
	maxBundle = 512 << 20 // a content bundle
)

// errNotFound is a 404, which for channel.json means nothing is published yet
var errNotFound = errors.New("not found")

// baseURL is the mirror, checked, or DefaultBase
func baseURL(mirror string) (string, error) {
	if mirror == "" {
		return DefaultBase, nil
	}
	u, err := url.Parse(mirror)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("content.mirror: %q isn't an http(s) URL; fix it with bud config set content.mirror <url>, or unset it to use GitHub", mirror)
	}

	return strings.TrimSuffix(mirror, "/"), nil
}

func releaseURL(base, tag, name string) string { return base + "/" + tag + "/" + name }

// get opens url; the caller closes the body. Every download is checked
// against a signature, so a plain-http mirror can't slip anything in
func get(ctx context.Context, url string, limit int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("download %s: %w", url, errNotFound)
		}
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}

	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, limit), resp.Body}, nil
}

func getAll(ctx context.Context, url string) ([]byte, error) {
	body, err := get(ctx, url, maxJSON)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	b, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}

	return b, nil
}

// fetchChannel downloads channel.json and checks its signature
func fetchChannel(ctx context.Context, base string) (*content.Channel, error) {
	msg, err := getAll(ctx, releaseURL(base, ChannelTag, content.ChannelName))
	if errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("%w: no content has been published there yet", err)
	}
	if err != nil {
		return nil, err
	}
	sig, err := getAll(ctx, releaseURL(base, ChannelTag, content.ChannelName+".sig"))
	if err != nil {
		return nil, err
	}

	return content.VerifyChannel(msg, sig)
}

// fetchManifest downloads version's content.json and checks its signature
// and that it is the version asked for, so a validly signed manifest of
// another version can't be swapped in
func fetchManifest(ctx context.Context, base, version string) (*content.Manifest, error) {
	tag := "content-" + version
	msg, err := getAll(ctx, releaseURL(base, tag, content.ManifestName))
	if err != nil {
		return nil, err
	}
	sig, err := getAll(ctx, releaseURL(base, tag, content.ManifestName+".sig"))
	if err != nil {
		return nil, err
	}
	m, err := content.VerifyManifest(msg, sig)
	if err != nil {
		return nil, err
	}
	if m.Version != version {
		return nil, fmt.Errorf("content.json of release %s is for version %s; the release is broken, so nothing changed", tag, m.Version)
	}

	return m, nil
}

// fetchBundle downloads m's bundle and unpacks it into dst, checking every
// file against m
func fetchBundle(ctx context.Context, base string, m *content.Manifest, dst string) error {
	url := releaseURL(base, "content-"+m.Version, content.BundleName(m.Version))
	body, err := get(ctx, url, maxBundle)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := content.Unpack(body, m, dst); err != nil {
		return fmt.Errorf("unpack %s: %w", url, err)
	}

	return nil
}
