package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// the command needs its credentials from op run and a loopback address,
// since devices reach it only through the MCP gateway
func TestMailMCPChecksItsSetup(t *testing.T) {
	var served string
	orig := serveMail
	serveMail = func(_ context.Context, addr string, _ http.Handler) error { served = addr; return nil }
	t.Cleanup(func() { serveMail = orig })

	t.Setenv("BUD_MAIL_USER", "")
	t.Setenv("BUD_MAIL_PASSWORD", "")
	if err := runMailMCP(&Globals{}, nil, "127.0.0.1:8796", "imap.example.com:993"); err == nil || !strings.Contains(err.Error(), "BUD_MAIL_PASSWORD") {
		t.Fatalf("no credentials: err = %v", err)
	}
	t.Setenv("BUD_MAIL_USER", "u")
	t.Setenv("BUD_MAIL_PASSWORD", "p")
	for _, bad := range []struct{ listen, imap string }{
		{"0.0.0.0:8796", "imap.example.com:993"},
		{"100.64.0.1:8796", "imap.example.com:993"},
		{"127.0.0.1:8796", "imap.example.com"},
	} {
		if err := runMailMCP(&Globals{}, nil, bad.listen, bad.imap); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if err := runMailMCP(&Globals{}, nil, "127.0.0.1:8796", "imap.example.com:993"); err != nil {
		t.Fatal(err)
	}
	if served != "127.0.0.1:8796" {
		t.Errorf("served on %q", served)
	}
	if err := runMailMCP(&Globals{}, []string{"x"}, "127.0.0.1:8796", "imap.example.com:993"); err == nil {
		t.Error("an argument was accepted")
	}
}
