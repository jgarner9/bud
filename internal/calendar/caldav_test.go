package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDAV is a small CalDAV server laid out like iCloud's: a principal, a
// calendar home, a calendar for events and a reminders-only list. It
// doesn't expand repeating events, so tests hand it occurrences as they are
type fakeDAV struct {
	t      *testing.T
	mu     sync.Mutex
	objs   map[string]string // path to calendar object
	etags  map[string]int
	puts   []*http.Request
	report string // the last REPORT body
	// noExpand answers a REPORT that asks for expansion with 501
	noExpand bool
}

const (
	principalPath = "/123/principal/"
	homePath      = "/123/calendars/"
	workPath      = "/123/calendars/work/"
)

func newFake(t *testing.T) (*fakeDAV, *Client) {
	f := &fakeDAV{t: t, objs: map[string]string{}, etags: map[string]int{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL+"/", "jason", "app-pass", chicago)
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }

	return f, c
}

func (f *fakeDAV) put(path, data string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[path] = data
	f.etags[path]++
}

func (f *fakeDAV) etag(path string) string { return fmt.Sprintf(`"%d"`, f.etags[path]) }

func ms(responses ...string) string {
	return `<?xml version="1.0"?><multistatus xmlns="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` + strings.Join(responses, "") + `</multistatus>`
}

func (f *fakeDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, p, ok := r.BasicAuth(); !ok || u != "jason" || p != "app-pass" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path
	switch {
	case r.Method == "PROPFIND" && path == "/":
		w.WriteHeader(207)
		fmt.Fprint(w, ms(`<response><href>/</href><propstat><prop><current-user-principal><href>`+principalPath+`</href></current-user-principal></prop><status>HTTP/1.1 200 OK</status></propstat></response>`))
	case r.Method == "PROPFIND" && path == principalPath:
		w.WriteHeader(207)
		fmt.Fprint(w, ms(`<response><href>`+principalPath+`</href><propstat><prop><C:calendar-home-set><href>`+homePath+`</href></C:calendar-home-set></prop><status>HTTP/1.1 200 OK</status></propstat></response>`))
	case r.Method == "PROPFIND" && path == homePath:
		if r.Header.Get("Depth") != "1" {
			f.t.Errorf("home PROPFIND with Depth %q", r.Header.Get("Depth"))
		}
		w.WriteHeader(207)
		fmt.Fprint(w, ms(
			`<response><href>`+homePath+`</href><propstat><prop><resourcetype><collection/></resourcetype></prop></propstat></response>`,
			`<response><href>`+workPath+`</href><propstat><prop><resourcetype><collection/><C:calendar/></resourcetype><displayname>Work</displayname><C:supported-calendar-component-set><C:comp name="VEVENT"/></C:supported-calendar-component-set></prop></propstat></response>`,
			`<response><href>/123/calendars/tasks/</href><propstat><prop><resourcetype><collection/><C:calendar/></resourcetype><displayname>Reminders ⚠️</displayname><C:supported-calendar-component-set><C:comp name="VTODO"/></C:supported-calendar-component-set></prop></propstat></response>`,
		))
	case r.Method == "REPORT" && path == workPath:
		f.report = string(body)
		if f.noExpand && strings.Contains(f.report, "expand") {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		var rs []string
		for p, data := range f.objs {
			esc := strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace(data)
			rs = append(rs, `<response><href>`+p+`</href><propstat><prop><getetag>`+f.etag(p)+`</getetag><C:calendar-data>`+esc+`</C:calendar-data></prop></propstat></response>`)
		}
		w.WriteHeader(207)
		fmt.Fprint(w, ms(rs...))
	case r.Method == "PUT":
		_, exists := f.objs[path]
		if r.Header.Get("If-None-Match") == "*" && exists || r.Header.Get("If-Match") != "" && r.Header.Get("If-Match") != f.etag(path) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		f.puts = append(f.puts, r)
		f.objs[path] = string(body)
		f.etags[path]++
		w.WriteHeader(http.StatusCreated)
	case r.Method == "GET":
		data, ok := f.objs[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("ETag", f.etag(path))
		fmt.Fprint(w, data)
	case r.Method == "DELETE":
		if r.Header.Get("If-Match") != f.etag(path) {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		delete(f.objs, path)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func event(summary, start, end string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:" + summary + "\r\nSUMMARY:" + summary + "\r\nDTSTART:" + start + "\r\nDTEND:" + end + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
}

func TestClientFindsTheCalendars(t *testing.T) {
	_, c := newFake(t)
	cals, err := c.Calendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 2 || cals[0].Name != "Reminders ⚠️" || cals[0].Events || cals[1].Name != "Work" || !cals[1].Events || cals[1].ID != workPath {
		t.Errorf("calendars %+v", cals)
	}
}

func TestClientListsEventsInRange(t *testing.T) {
	f, c := newFake(t)
	f.put(workPath+"b.ics", event("Standup", "20261117T150000Z", "20261117T151500Z"))
	f.put(workPath+"a.ics", event("Dentist", "20261116T150000Z", "20261116T160000Z"))
	f.put(workPath+"old.ics", event("Last week", "20261101T150000Z", "20261101T160000Z"))
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, chicago)
	evs, err := c.Events(context.Background(), "", start, start.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Title != "Dentist" || evs[1].Title != "Standup" || evs[0].ID != workPath+"a.ics" || evs[0].Calendar != "Work" {
		t.Fatalf("events %+v", evs)
	}
	if evs[0].Start.Location() != chicago {
		t.Errorf("start in %v", evs[0].Start.Location())
	}
	if !strings.Contains(f.report, `<c:expand start="20261116T060000Z" end="20261123T060000Z"/>`) || !strings.Contains(f.report, `<c:comp-filter name="VEVENT">`) {
		t.Errorf("REPORT body %s", f.report)
	}
	f.noExpand = true
	f.put(workPath+"weekly.ics", strings.Replace(event("Weekly", "20261005T150000Z", "20261005T160000Z"), "DTEND", "RRULE:FREQ=WEEKLY\r\nDTEND", 1))
	evs, err = c.Events(context.Background(), "", start, start.AddDate(0, 0, 7))
	if err != nil || len(evs) != 3 || evs[0].Repeats != "FREQ=WEEKLY" || strings.Contains(f.report, "expand") {
		t.Errorf("without expansion: %+v %v", evs, err)
	}
	if _, err := c.Events(context.Background(), "Home", start, start.AddDate(0, 0, 1)); err == nil || !strings.Contains(err.Error(), "Work") {
		t.Errorf("an unknown calendar: err = %v", err)
	}
}

func TestClientCreatesMovesAndDeletes(t *testing.T) {
	f, c := newFake(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 18, 14, 0, 0, 0, chicago)
	ev, err := c.Create(ctx, "work", NewEvent{Title: "Plumber", Start: start, End: start.Add(time.Hour), Alarms: []int{30}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ev.ID, workPath) || !strings.HasSuffix(ev.ID, ".ics") || ev.Title != "Plumber" || ev.Alarms[0] != 30 {
		t.Fatalf("created %+v", ev)
	}
	if f.puts[0].Header.Get("If-None-Match") != "*" || !strings.HasPrefix(f.puts[0].Header.Get("Content-Type"), "text/calendar") {
		t.Errorf("create headers %v", f.puts[0].Header)
	}
	if _, err := c.Create(ctx, "Reminders ⚠️", NewEvent{Title: "x", Start: start, End: start.Add(time.Hour)}); err == nil {
		t.Error("an event went into a reminders-only list")
	}

	// moving keeps the length when no end is given
	later := start.Add(26 * time.Hour)
	moved, err := c.Move(ctx, ev.ID, later, time.Time{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !moved.Start.Equal(later) || moved.End.Sub(moved.Start) != time.Hour || moved.Alarms[0] != 30 {
		t.Errorf("moved %+v", moved)
	}
	if f.puts[1].Header.Get("If-Match") != `"1"` {
		t.Errorf("move If-Match %q", f.puts[1].Header.Get("If-Match"))
	}

	// a change on the server since the read is refused, not overwritten
	f.mu.Lock()
	f.etags[ev.ID] = 9
	f.mu.Unlock()
	if _, _, err := c.do(ctx, "DELETE", mustURL(t, c, ev.ID), map[string]string{"If-Match": `"2"`}, ""); err == nil || !strings.Contains(err.Error(), "changed on the server") {
		t.Errorf("a stale ETag: err = %v", err)
	}

	gone, err := c.Delete(ctx, ev.ID)
	if err != nil || gone.Title != "Plumber" {
		t.Fatalf("deleted %+v %v", gone, err)
	}
	if _, ok := f.objs[ev.ID]; ok {
		t.Error("the event is still there")
	}

	f.put(workPath+"weekly.ics", strings.Replace(event("Weekly", "20261117T150000Z", "20261117T160000Z"), "DTEND", "RRULE:FREQ=WEEKLY\r\nDTEND", 1))
	if _, err := c.Delete(ctx, workPath+"weekly.ics"); err == nil || !strings.Contains(err.Error(), "repeat") {
		t.Errorf("deleting a repeating event: err = %v", err)
	}
	for _, bad := range []string{"/elsewhere/x.ics", workPath + "../../x.ics", workPath + "x", "x.ics"} {
		if _, err := c.Delete(ctx, bad); err == nil || !strings.Contains(err.Error(), "isn't an event id") {
			t.Errorf("id %q: err = %v", bad, err)
		}
	}
}

func mustURL(t *testing.T, c *Client, id string) *url.URL {
	u, err := c.eventURL(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

func TestClientWrongPassword(t *testing.T) {
	_, c := newFake(t)
	c.pass = "wrong"
	if _, err := c.Calendars(context.Background()); err == nil || !strings.Contains(err.Error(), "app-specific password") {
		t.Errorf("err = %v", err)
	}
}

// credentials go only to https, or http on loopback, and only to the
// server's own domain
func TestClientTrustsOnlyItsDomain(t *testing.T) {
	c, err := NewClient("https://caldav.icloud.com/", "u", "p", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]bool{
		"https://caldav.icloud.com/x":     true,
		"https://p42-caldav.icloud.com/x": true,
		"https://evil.example.com/x":      false,
		"https://icloud.com/x":            true,
		"https://icloud.com.evil.com/x":   false,
		"http://p42-caldav.icloud.com/x":  false,
		"http://127.0.0.1:9/x":            false,
	} {
		if got := c.trusted(mustParse(t, raw)); got != want {
			t.Errorf("%s: trusted = %v", raw, got)
		}
	}
	// another server's neighbours under its parent domain aren't trusted
	other, err := NewClient("https://calendar.co.uk/", "u", "p", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if other.trusted(mustParse(t, "https://attacker.co.uk/")) || other.trusted(mustParse(t, "https://p1-caldav.icloud.com/")) {
		t.Error("a host beside calendar.co.uk was trusted")
	}
	if _, err := NewClient("http://caldav.icloud.com/", "u", "p", time.UTC); err == nil {
		t.Error("plain http was accepted")
	}
	if _, err := NewClient("https://localhost/", "u", "p", time.UTC); err != nil {
		t.Errorf("https to a single-label host: %v", err)
	}
	single, _ := NewClient("https://localhost/", "u", "p", time.UTC)
	if single.trusted(mustParse(t, "https://other/")) {
		t.Error("a single-label host trusted a sibling")
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

// a redirect that would turn a DAV method into a GET is refused, not taken
// for success
func TestClientRefusesMethodChangingRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Redirect(w, r, "/moved", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL+"/", "u", "p", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.do(context.Background(), "DELETE", mustParse(t, srv.URL+"/x.ics"), nil, ""); err == nil || !strings.Contains(err.Error(), "turns DELETE into GET") {
		t.Errorf("err = %v", err)
	}
}
