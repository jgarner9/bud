package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"jgarner.dev/bud/internal/calendar"
)

func init() {
	RegisterCommand(CalendarMCP())
}

// CalendarMCP runs the home server's calendar MCP server over CalDAV
// (Suite D82); see docs/internals/calendar.md
func CalendarMCP() *Command {
	var listen, url, tz string
	return &Command{
		Name:  "calendar-mcp",
		Usage: "Runs the home server's calendar MCP server over CalDAV, behind the MCP gateway",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&listen, "listen", "127.0.0.1:8791", "the loopback host:port to serve MCP on, at /mcp")
			fs.StringVar(&url, "url", "https://caldav.icloud.com/", "the CalDAV server")
			fs.StringVar(&tz, "tz", "", "the zone for times given without one, such as America/Chicago (default the system's)")
		},
		Run: func(g *Globals, args []string) error { return runCalendarMCP(g, args, listen, url, tz) },
	}
}

// serveCalendar is swappable so tests stop before listening
var serveCalendar = calendar.Serve

func runCalendarMCP(g *Globals, args []string, listen, url, tz string) error {
	if len(args) > 0 {
		return usageErrorf("calendar-mcp takes no arguments")
	}
	if err := calendar.CheckListen(listen); err != nil {
		return usageErrorf("%v", err)
	}
	loc := time.Local
	if tz != "" {
		l, err := time.LoadLocation(tz)
		if err != nil {
			return usageErrorf("--tz %q isn't a zone Go knows, such as America/Chicago", tz)
		}
		loc = l
	}
	c, err := calendar.NewClient(url, os.Getenv("BUD_CALDAV_USER"), os.Getenv("BUD_CALDAV_PASSWORD"), loc)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	g.Logf("calendar-mcp: serving %s on http://%s/mcp, times in %s", url, listen, loc)
	if err := serveCalendar(ctx, listen, calendar.NewServer(c, loc)); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("calendar-mcp: %w", err)
	}

	return nil
}
