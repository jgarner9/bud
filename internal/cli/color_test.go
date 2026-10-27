package cli

import (
	"bytes"
	"os"
	"testing"
)

func TestColorOnlyOnTerminals(t *testing.T) {
	var buf bytes.Buffer
	if got := paint(&buf, ansiRed, "bud:"); got != "bud:" {
		t.Fatalf("a buffer got colour: %q", got)
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if got := paint(f, ansiRed, "bud:"); got != "bud:" {
		t.Fatalf("a file got colour: %q", got)
	}
	t.Setenv("NO_COLOR", "1")
	if colorOn(os.Stdout) {
		t.Fatal("NO_COLOR ignored")
	}
}

func TestStatusStyle(t *testing.T) {
	for status, want := range map[string]string{"ok": ansiGreen, "warn": ansiYellow, "fail": ansiRed} {
		if got := statusStyle(status); got != want {
			t.Errorf("%s: %q", status, got)
		}
	}
}
