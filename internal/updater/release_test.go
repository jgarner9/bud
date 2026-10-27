package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGitHub serves the two release endpoints; a nil body is a 404
func fakeGitHub(t *testing.T, latest, nightly string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "bud/") {
			t.Errorf("no User-Agent: %q", r.Header.Get("User-Agent"))
		}
		var body string
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			body = latest
		case "/repos/" + Repo + "/releases/tags/" + NightlyTag:
			body = nightly
		}
		if body == "" {
			http.NotFound(w, r)
			return
		}
		if body == "500" {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	orig := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = orig })
}

const stableJSON = `{"tag_name": "v0.6.0", "name": "v0.6.0", "assets": [
	{"name": "bud_darwin_arm64.tar.gz", "browser_download_url": "https://example.com/a.tar.gz"},
	{"name": "checksums.txt", "browser_download_url": "https://example.com/checksums.txt"}]}`

const nightlyJSON = `{"tag_name": "nightly", "name": "0.6.1-nightly.202610280300", "assets": []}`

func TestLatest(t *testing.T) {
	fakeGitHub(t, stableJSON, nightlyJSON)
	ctx := context.Background()

	r, err := Latest(ctx, "stable")
	if err != nil || r.Version != "0.6.0" {
		t.Fatalf("stable: %+v, %v", r, err)
	}
	if a, ok := r.Asset(ArchiveName("darwin", "arm64")); !ok || a.URL != "https://example.com/a.tar.gz" {
		t.Errorf("asset = %+v, %v", a, ok)
	}
	if _, ok := r.Asset("nope"); ok {
		t.Error("found a missing asset")
	}

	// nightly's version comes from its name, since its tag never changes
	if r, err := Latest(ctx, "nightly"); err != nil || r.Version != "0.6.1-nightly.202610280300" {
		t.Fatalf("nightly: %+v, %v", r, err)
	}
	if _, err := Latest(ctx, "beta"); err == nil {
		t.Error("expected an error for an unknown channel")
	}
}

func TestLatestErrors(t *testing.T) {
	ctx := context.Background()

	fakeGitHub(t, "", "")
	if _, err := Latest(ctx, "nightly"); !errors.Is(err, ErrNoRelease) {
		t.Errorf("404: %v", err)
	}

	fakeGitHub(t, "500", "")
	if _, err := Latest(ctx, "stable"); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error status: %v", err)
	}

	fakeGitHub(t, `{"tag_name": "latest-and-greatest"}`, "")
	if _, err := Latest(ctx, "stable"); err == nil || !strings.Contains(err.Error(), "no version bud can read") {
		t.Errorf("unreadable version: %v", err)
	}

	fakeGitHub(t, `{nope`, "")
	if _, err := Latest(ctx, "stable"); err == nil {
		t.Error("expected an error for bad JSON")
	}
}

func TestCheck(t *testing.T) {
	fakeGitHub(t, stableJSON, nightlyJSON)
	ctx := context.Background()

	for _, tt := range []struct {
		channel, current string
		newer, dev       bool
	}{
		{"stable", "0.5.1", true, false},
		{"stable", "0.6.0", false, false},
		{"stable", "0.7.0", false, false},
		// a nightly is newer than the release it follows, older than the next
		{"nightly", "0.6.0", true, false},
		{"nightly", "0.6.1-nightly.202610270300", true, false},
		{"nightly", "0.6.1", false, false},
		// a local build has no release version, so it's never offered one
		{"stable", "dev", false, true},
	} {
		s, err := Check(ctx, tt.channel, tt.current)
		if err != nil {
			t.Fatalf("%s %s: %v", tt.channel, tt.current, err)
		}
		if s.Newer != tt.newer || s.Dev != tt.dev {
			t.Errorf("%s %s: newer %v dev %v, want %v %v", tt.channel, tt.current, s.Newer, s.Dev, tt.newer, tt.dev)
		}
	}
}
