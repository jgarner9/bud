package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"jgarner.dev/bud/internal/mailingest"
	models "jgarner.dev/bud/internal/registry"
)

func TestMailIngestChecksItsInputsAndUsesOnlyLocalModels(t *testing.T) {
	origReg, origDo := mailIngestRegistry, doMailIngest
	t.Cleanup(func() { mailIngestRegistry, doMailIngest = origReg, origDo })
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("BUD_MAIL_USER", "someone@example.com")
	t.Setenv("BUD_MAIL_PASSWORD", "app-pass")
	var got *mailingest.Run
	doMailIngest = func(_ context.Context, r *mailingest.Run) (mailingest.Result, error) {
		got = r
		return mailingest.Result{Summarized: 2, Started: []string{"INBOX"}}, nil
	}
	cloud := &models.Registry{
		Providers: map[string]models.Provider{"go": {API: "openai", Endpoint: "https://go.example/v1", Key: "op://vault/go/key"}},
		Models:    map[string]models.Model{"m": {Provider: "go", Name: "m"}},
		Roles:     map[string]models.Role{"curator": {Pool: []string{"m"}}},
	}
	mailIngestRegistry = func(string) (*models.Registry, error) { return cloud, nil }
	f := mailIngestFlags{imap: "127.0.0.1:1", folders: "INBOX", limit: 25}
	if err := runMailIngest(&Globals{}, nil, f); err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("a registry with no local curator model was accepted: %v", err)
	}
	local := &models.Registry{
		Providers: map[string]models.Provider{"qwen": {API: "openai", Endpoint: "http://qwen.invalid:8080/v1", Local: true}},
		Models:    map[string]models.Model{"q": {Provider: "qwen", Name: "qwen-35b"}},
		Roles:     map[string]models.Role{"curator": {Pool: []string{"q"}}},
	}
	mailIngestRegistry = func(string) (*models.Registry, error) { return local, nil }
	var out bytes.Buffer
	f.folders = " INBOX , Archive ,"
	if err := runMailIngest(&Globals{stdout: &out}, nil, f); err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Opts.Folders) != 2 || got.Opts.Folders[1] != "Archive" || got.Opts.Backfill != 0 {
		t.Fatalf("run %+v", got)
	}
	if want := "2 summarized, 0 failed, 0 already stored; began INBOX at its newest message"; !strings.Contains(out.String(), want) {
		t.Errorf("output %q", out.String())
	}
	for name, bad := range map[string]mailIngestFlags{
		"no folder":         {imap: f.imap, folders: " , ", limit: 25},
		"limit too big":     {imap: f.imap, folders: "INBOX", limit: 500},
		"backfill over cap": {imap: f.imap, folders: "INBOX", limit: 5, backfill: 6},
	} {
		if err := runMailIngest(&Globals{}, nil, bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := runMailIngest(&Globals{}, []string{"x"}, f); err == nil {
		t.Error("an argument was accepted")
	}
	t.Setenv("BUD_MAIL_PASSWORD", "")
	if err := runMailIngest(&Globals{}, nil, f); err == nil || !strings.Contains(err.Error(), "BUD_MAIL_PASSWORD") {
		t.Errorf("no password: %v", err)
	}
}
