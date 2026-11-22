package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"jgarner.dev/bud/internal/kbsite"
)

func TestKBSiteChecksItsFlags(t *testing.T) {
	var got *kbsite.Builder
	orig := serveKBSite
	serveKBSite = func(_ context.Context, _ string, b *kbsite.Builder, _ time.Duration, _ bool, _ func(string, ...any)) error {
		got = b
		return nil
	}
	t.Cleanup(func() { serveKBSite = orig })
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	ok := kbSiteOpts{listen: "127.0.0.1:8799", interval: time.Minute}
	for name, o := range map[string]kbSiteOpts{
		"public address": {listen: "0.0.0.0:8799", interval: time.Minute},
		"localhost":      {listen: "localhost:8799", interval: time.Minute},
		"no port":        {listen: "127.0.0.1", interval: time.Minute},
		"busy interval":  {listen: "127.0.0.1:8799", interval: time.Second},
	} {
		if err := runKBSite(&Globals{}, nil, o); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := runKBSite(&Globals{}, []string{"x"}, ok); err == nil {
		t.Error("an argument was accepted")
	}
	var stderr bytes.Buffer
	if err := runKBSite(&Globals{stderr: &stderr}, nil, ok); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "kb-site: serving") {
		t.Errorf("stderr = %q, want the start line", stderr.String())
	}
	if got == nil || !strings.HasSuffix(got.Repo.Dir, "bud/kb") || !strings.HasSuffix(got.Dir, "bud/kb-site") {
		t.Errorf("builder = %+v", got)
	}
}
