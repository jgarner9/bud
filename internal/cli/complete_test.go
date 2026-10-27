package cli

import (
	"strings"
	"testing"
)

func completions(t *testing.T, args ...string) []string {
	t.Helper()
	var b strings.Builder
	if err := complete(&b, CommandRegistry, args); err != nil {
		t.Fatal(err)
	}
	return strings.Fields(b.String())
}

func has(list []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, l := range list {
			found = found || l == w
		}
		if !found {
			return false
		}
	}
	return true
}

func TestComplete(t *testing.T) {
	layoutHome(t)
	if got := completions(t, ""); !has(got, "open", "launch", "config", "kit") || has(got, "__complete") {
		t.Errorf("commands: %v", got)
	}
	if got := completions(t, "la"); !has(got, "lang", "launch", "layout") || has(got, "open") {
		t.Errorf("prefix: %v", got)
	}
	if got := completions(t, "--"); !has(got, "--verbose", "--layout") {
		t.Errorf("global flags: %v", got)
	}
	if got := completions(t, "--layout", ""); !has(got, "default", "focus") {
		t.Errorf("--layout values: %v", got)
	}
	if got := completions(t, "--layout", "focus", "lau"); !has(got, "launch") {
		t.Errorf("past a global flag's value: %v", got)
	}
	if got := completions(t, "kit", ""); !has(got, "sync", "status", "secret", "adopt") {
		t.Errorf("kit subs: %v", got)
	}
	if got := completions(t, "kit", "sync", "--"); !has(got, "--dry-run") {
		t.Errorf("sub flags: %v", got)
	}
	if got := completions(t, "launch", "--terminal", ""); !has(got, "kitty", "ghostty") {
		t.Errorf("--terminal values: %v", got)
	}
	if got := completions(t, "config", "get", "update."); !has(got, "update.channel", "update.auto") {
		t.Errorf("config keys: %v", got)
	}
	if got := completions(t, "layout", "set-default", ""); !has(got, "default", "focus") {
		t.Errorf("layout names: %v", got)
	}
	if got := completions(t, "kit", "rm", ""); !has(got, "context7") {
		t.Errorf("kit names: %v", got)
	}
	// a path: nothing, so the shell completes file names
	if got := completions(t, "./src", ""); len(got) != 0 {
		t.Errorf("after a path: %v", got)
	}
}
