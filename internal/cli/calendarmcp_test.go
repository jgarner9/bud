package cli

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// the command needs its credentials from op run and a loopback address,
// since devices reach it only through the MCP gateway
func TestCalendarMCPChecksItsSetup(t *testing.T) {
	var served string
	orig := serveCalendar
	serveCalendar = func(_ context.Context, addr string, _ http.Handler) error { served = addr; return nil }
	t.Cleanup(func() { serveCalendar = orig })

	t.Setenv("BUD_CALDAV_USER", "")
	t.Setenv("BUD_CALDAV_PASSWORD", "")
	if err := runCalendarMCP(&Globals{}, nil, "127.0.0.1:8791", "https://caldav.example.com/", ""); err == nil || !strings.Contains(err.Error(), "BUD_CALDAV_PASSWORD") {
		t.Fatalf("no credentials: err = %v", err)
	}
	t.Setenv("BUD_CALDAV_USER", "u")
	t.Setenv("BUD_CALDAV_PASSWORD", "p")
	for _, bad := range []struct{ listen, url, tz string }{
		{"0.0.0.0:8791", "https://caldav.example.com/", ""},
		{"100.64.0.1:8791", "https://caldav.example.com/", ""},
		{"127.0.0.1:8791", "http://caldav.example.com/", ""},
		{"127.0.0.1:8791", "https://caldav.example.com/", "Not/AZone"},
	} {
		if err := runCalendarMCP(&Globals{}, nil, bad.listen, bad.url, bad.tz); err == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
	if err := runCalendarMCP(&Globals{}, nil, "127.0.0.1:8791", "https://caldav.example.com/", "America/Chicago"); err != nil {
		t.Fatal(err)
	}
	if served != "127.0.0.1:8791" {
		t.Errorf("served on %q", served)
	}
	if err := runCalendarMCP(&Globals{}, []string{"x"}, "127.0.0.1:8791", "https://caldav.example.com/", ""); err == nil {
		t.Error("an argument was accepted")
	}
}
