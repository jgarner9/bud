package calendar

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Client talks CalDAV (RFC 4791) to one account, such as iCloud's
type Client struct {
	base *url.URL
	user string
	pass string
	http *http.Client
	loc  *time.Location
	now  func() time.Time

	mu   sync.Mutex
	home *url.URL // the calendar home, found once
}

// NewClient checks the server URL and credentials. Credentials go only to
// https, or to http on loopback (for tests), and only to the base host, or
// for iCloud to another icloud.com host (it answers from p<n>-caldav hosts
// beside caldav.icloud.com)
func NewClient(rawURL, user, pass string, loc *time.Location) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("the CalDAV URL %q isn't a URL such as https://caldav.icloud.com/", rawURL)
	}
	if user == "" || pass == "" {
		return nil, errors.New("no CalDAV credentials: set BUD_CALDAV_USER and BUD_CALDAV_PASSWORD (op run fills them from the Bud vault)")
	}
	c := &Client{base: u, user: user, pass: pass, loc: loc, now: time.Now}
	if !c.trusted(u) {
		return nil, fmt.Errorf("the CalDAV URL %q must be https, so the password never goes in the clear", rawURL)
	}
	c.http = &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !c.trusted(req.URL) {
				return fmt.Errorf("refusing a redirect to %s, outside the CalDAV server's domain", req.URL.Host)
			}
			// a 301, 302 or 303 turns PROPFIND, REPORT, PUT and DELETE into a
			// GET without its body, which would look like success
			if req.Method != via[0].Method {
				return fmt.Errorf("refusing a redirect that turns %s into %s", via[0].Method, req.Method)
			}
			req.SetBasicAuth(c.user, c.pass)
			return nil
		},
	}

	return c, nil
}

// trusted says whether credentials may go to u
func (c *Client) trusted(u *url.URL) bool {
	host := u.Hostname()
	switch u.Scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return false
		}
	default:
		return false
	}
	host, base := strings.ToLower(host), strings.ToLower(c.base.Hostname())
	if host == base {
		return true
	}
	// iCloud hands each account to a p<n>-caldav host beside caldav.icloud.com;
	// any other server gets its own host only
	return icloud(base) && icloud(host)
}

func icloud(host string) bool { return host == "icloud.com" || strings.HasSuffix(host, ".icloud.com") }

// do sends one request with the credentials and returns the body of a 2xx
func (c *Client) do(ctx context.Context, method string, u *url.URL, hdr map[string]string, body string) (*http.Response, []byte, error) {
	if !c.trusted(u) {
		return nil, nil, fmt.Errorf("refusing to send credentials to %s, outside the CalDAV server's domain", u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.SetBasicAuth(c.user, c.pass)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s: %w", method, u.Path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s: %w", method, u.Path, err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return resp, nil, errors.New("the CalDAV server refused the credentials: check the app-specific password in the Bud vault")
	case resp.StatusCode == http.StatusPreconditionFailed:
		return resp, nil, errors.New("it changed on the server since it was read; list it again and retry")
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return resp, nil, fmt.Errorf("%s %s: the server answered %s", method, u.Path, resp.Status)
	}

	return resp, b, nil
}

// the WebDAV multistatus answer (RFC 4918 13), with the properties bud reads
type multistatus struct {
	Responses []struct {
		Href     string `xml:"DAV: href"`
		Propstat []struct {
			Status string `xml:"DAV: status"`
			Prop   struct {
				Principal    hrefProp `xml:"DAV: current-user-principal"`
				Home         hrefProp `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
				DisplayName  string   `xml:"DAV: displayname"`
				ResourceType struct {
					Calendar *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
				} `xml:"DAV: resourcetype"`
				Components struct {
					Comps []struct {
						Name string `xml:"name,attr"`
					} `xml:"urn:ietf:params:xml:ns:caldav comp"`
				} `xml:"urn:ietf:params:xml:ns:caldav supported-calendar-component-set"`
				ETag string `xml:"DAV: getetag"`
				Data string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

type hrefProp struct {
	Href string `xml:"DAV: href"`
}

func (c *Client) propfind(ctx context.Context, u *url.URL, depth, props string) (*multistatus, *url.URL, error) {
	body := `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop>` + props + `</d:prop></d:propfind>`
	resp, b, err := c.do(ctx, "PROPFIND", u, map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"}, body)
	if err != nil {
		return nil, nil, err
	}
	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, nil, fmt.Errorf("PROPFIND %s: the answer isn't WebDAV XML: %w", u.Path, err)
	}

	return &ms, resp.Request.URL, nil
}

// resolve reads an href against the URL that answered
func resolve(at *url.URL, href string) (*url.URL, error) {
	h, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil, fmt.Errorf("the server gave a bad href %q", href)
	}

	return at.ResolveReference(h), nil
}

// calendarHome finds the account's calendar home (RFC 6764 6, RFC 4791 6.2.1)
func (c *Client) calendarHome(ctx context.Context) (*url.URL, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.home != nil {
		return c.home, nil
	}
	ms, at, err := c.propfind(ctx, c.base, "0", `<d:current-user-principal/>`)
	if err != nil {
		return nil, fmt.Errorf("find the CalDAV principal: %w", err)
	}
	href := ""
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.Principal.Href != "" {
				href = ps.Prop.Principal.Href
			}
		}
	}
	if href == "" {
		return nil, errors.New("find the CalDAV principal: the server named none")
	}
	principal, err := resolve(at, href)
	if err != nil {
		return nil, err
	}
	ms, at, err = c.propfind(ctx, principal, "0", `<c:calendar-home-set/>`)
	if err != nil {
		return nil, fmt.Errorf("find the calendar home: %w", err)
	}
	href = ""
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if ps.Prop.Home.Href != "" {
				href = ps.Prop.Home.Href
			}
		}
	}
	if href == "" {
		return nil, errors.New("find the calendar home: the server named none")
	}
	home, err := resolve(at, href)
	if err != nil {
		return nil, err
	}
	c.home = home

	return home, nil
}

// Calendar is one calendar collection
type Calendar struct {
	ID   string `json:"id"` // the collection's path
	Name string `json:"name"`
	// Events says it holds events; a reminders-only (VTODO) list doesn't
	Events bool `json:"events"`
	url    *url.URL
}

// Calendars lists the account's calendars, sorted by name
func (c *Client) Calendars(ctx context.Context) ([]Calendar, error) {
	home, err := c.calendarHome(ctx)
	if err != nil {
		return nil, err
	}
	ms, at, err := c.propfind(ctx, home, "1", `<d:resourcetype/><d:displayname/><c:supported-calendar-component-set/>`)
	if err != nil {
		return nil, fmt.Errorf("list the calendars: %w", err)
	}
	var out []Calendar
	for _, r := range ms.Responses {
		u, err := resolve(at, r.Href)
		if err != nil {
			return nil, err
		}
		for _, ps := range r.Propstat {
			if ps.Prop.ResourceType.Calendar == nil {
				continue
			}
			cal := Calendar{ID: u.Path, Name: ps.Prop.DisplayName, url: u}
			if cal.Name == "" {
				cal.Name = strings.Trim(u.Path[strings.LastIndex(strings.TrimSuffix(u.Path, "/"), "/")+1:], "/")
			}
			// no component set means any component (RFC 4791 5.2.3)
			cal.Events = len(ps.Prop.Components.Comps) == 0
			for _, comp := range ps.Prop.Components.Comps {
				if strings.EqualFold(comp.Name, "VEVENT") {
					cal.Events = true
				}
			}
			out = append(out, cal)
		}
	}
	slices.SortFunc(out, func(a, b Calendar) int { return strings.Compare(a.Name+a.ID, b.Name+b.ID) })

	return out, nil
}

// find picks a calendar by name (case ignored) or id; empty means every
// calendar that holds events
func (c *Client) find(ctx context.Context, which string) ([]Calendar, error) {
	all, err := c.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	var out []Calendar
	for _, cal := range all {
		if which == "" && cal.Events || which != "" && (strings.EqualFold(cal.Name, which) || cal.ID == which) {
			out = append(out, cal)
		}
	}
	if len(out) == 0 {
		var names []string
		for _, cal := range all {
			if cal.Events {
				names = append(names, cal.Name)
			}
		}
		if which == "" {
			return nil, errors.New("the account has no calendar that holds events")
		}
		return nil, fmt.Errorf("no calendar is named %q; the calendars are %s", which, strings.Join(names, ", "))
	}
	if which != "" && len(out) > 1 {
		return nil, fmt.Errorf("%d calendars are named %q; give its id from list_calendars", len(out), which)
	}

	return out, nil
}

// Events lists the events that overlap [start, end), sorted by start, with
// repeating events expanded into their occurrences by the server
func (c *Client) Events(ctx context.Context, calendar string, start, end time.Time) ([]Event, error) {
	cals, err := c.find(ctx, calendar)
	if err != nil {
		return nil, err
	}
	s, e := utc(start), utc(end)
	query := func(data string) string {
		return `<?xml version="1.0" encoding="utf-8"?>` +
			`<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">` +
			`<d:prop><d:getetag/>` + data + `</d:prop>` +
			`<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT">` +
			`<c:time-range start="` + s + `" end="` + e + `"/>` +
			`</c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	}
	expanded := query(`<c:calendar-data><c:expand start="` + s + `" end="` + e + `"/></c:calendar-data>`)
	hdr := map[string]string{"Depth": "1", "Content-Type": "application/xml; charset=utf-8"}
	var out []Event
	for _, cal := range cals {
		resp, b, err := c.do(ctx, "REPORT", cal.url, hdr, expanded)
		if err != nil && resp != nil && (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotImplemented) {
			// a server that can't expand: repeating events then come back
			// once each, with their rule in Repeats
			resp, b, err = c.do(ctx, "REPORT", cal.url, hdr, query(`<c:calendar-data/>`))
		}
		if err != nil {
			return nil, fmt.Errorf("list the events in %s: %w", cal.Name, err)
		}
		var ms multistatus
		if err := xml.Unmarshal(b, &ms); err != nil {
			return nil, fmt.Errorf("list the events in %s: the answer isn't WebDAV XML: %w", cal.Name, err)
		}
		for _, r := range ms.Responses {
			u, err := resolve(resp.Request.URL, r.Href)
			if err != nil {
				return nil, err
			}
			for _, ps := range r.Propstat {
				if ps.Prop.Data == "" {
					continue
				}
				evs, err := eventsFrom(ps.Prop.Data, c.loc)
				if err != nil {
					return nil, fmt.Errorf("read %s: %w", u.Path, err)
				}
				for _, ev := range evs {
					// a server may hand back occurrences outside the range
					if ev.Repeats == "" && !overlaps(ev, start, end) {
						continue
					}
					ev.ID, ev.Calendar = u.Path, cal.Name
					ev.Start, ev.End = ev.Start.In(c.loc), ev.End.In(c.loc)
					out = append(out, ev)
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Event) int {
		if d := a.Start.Compare(b.Start); d != 0 {
			return d
		}
		return strings.Compare(a.Title+a.ID, b.Title+b.ID)
	})

	return out, nil
}

// overlaps says whether the event falls in [start, end): an event of no
// length counts when it starts inside, any other when it runs into it
func overlaps(ev Event, start, end time.Time) bool {
	if ev.End.Equal(ev.Start) {
		return !ev.Start.Before(start) && ev.Start.Before(end)
	}

	return ev.Start.Before(end) && ev.End.After(start)
}

// Create adds an event to the calendar and returns it
func (c *Client) Create(ctx context.Context, calendar string, e NewEvent) (Event, error) {
	cals, err := c.find(ctx, calendar)
	if err != nil {
		return Event{}, err
	}
	if calendar == "" && len(cals) > 1 {
		var names []string
		for _, cal := range cals {
			names = append(names, cal.Name)
		}
		return Event{}, fmt.Errorf("say which calendar: %s", strings.Join(names, ", "))
	}
	cal := cals[0]
	if !cal.Events {
		return Event{}, fmt.Errorf("%s holds no events", cal.Name)
	}
	uid, err := newUID()
	if err != nil {
		return Event{}, err
	}
	u := cal.url.JoinPath(uid + ".ics")
	data := buildEvent(uid, e, c.now())
	// If-None-Match: * never overwrites an event that's already there
	if _, _, err := c.do(ctx, "PUT", u, map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-None-Match": "*"}, data); err != nil {
		return Event{}, fmt.Errorf("create the event: %w", err)
	}
	evs, err := eventsFrom(data, c.loc)
	if err != nil {
		return Event{}, err
	}
	ev := evs[0]
	ev.ID, ev.Calendar = u.Path, cal.Name
	ev.Start, ev.End = ev.Start.In(c.loc), ev.End.In(c.loc)

	return ev, nil
}

// get reads one event's calendar object and its ETag
func (c *Client) get(ctx context.Context, id string) (*url.URL, string, string, error) {
	u, err := c.eventURL(ctx, id)
	if err != nil {
		return nil, "", "", err
	}
	resp, b, err := c.do(ctx, "GET", u, nil, "")
	if err != nil {
		return nil, "", "", fmt.Errorf("read the event: %w", err)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return nil, "", "", errors.New("read the event: the server gave no ETag, so a change couldn't be guarded against a newer edit")
	}

	return u, string(b), etag, nil
}

// eventURL turns an event id (a path from Events) into its URL, refusing
// one outside the calendar home
func (c *Client) eventURL(ctx context.Context, id string) (*url.URL, error) {
	home, err := c.calendarHome(ctx)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(id, "/") || !strings.HasSuffix(id, ".ics") || strings.Contains(id, "..") || !strings.HasPrefix(id, home.Path) {
		return nil, fmt.Errorf("%q isn't an event id; take one from list_events", id)
	}
	u := *home
	u.Path, u.RawPath = id, ""

	return &u, nil
}

// Move gives an event that doesn't repeat a new start and end
func (c *Client) Move(ctx context.Context, id string, start, end time.Time, allDay bool) (Event, error) {
	u, data, etag, err := c.get(ctx, id)
	if err != nil {
		return Event{}, err
	}
	if end.IsZero() {
		// keep its length
		evs, err := eventsFrom(data, c.loc)
		if err != nil {
			return Event{}, err
		}
		if len(evs) == 0 {
			return Event{}, errors.New("move the event: it holds no event")
		}
		was := evs[0]
		switch {
		case allDay && was.AllDay:
			days := int(was.End.Sub(was.Start).Round(24*time.Hour) / (24 * time.Hour))
			end = start.AddDate(0, 0, max(days, 1))
		case allDay:
			end = start.AddDate(0, 0, 1)
		default:
			end = start.Add(was.End.Sub(was.Start))
		}
	}
	moved, err := moveEvent(data, start, end, allDay, c.now())
	if err != nil {
		return Event{}, fmt.Errorf("move the event: %w", err)
	}
	if _, _, err := c.do(ctx, "PUT", u, map[string]string{"Content-Type": "text/calendar; charset=utf-8", "If-Match": etag}, moved); err != nil {
		return Event{}, fmt.Errorf("move the event: %w", err)
	}
	evs, err := eventsFrom(moved, c.loc)
	if err != nil {
		return Event{}, err
	}
	ev := evs[0]
	ev.ID = u.Path
	ev.Start, ev.End = ev.Start.In(c.loc), ev.End.In(c.loc)

	return ev, nil
}

// Delete removes an event that doesn't repeat, and returns what it was
func (c *Client) Delete(ctx context.Context, id string) (Event, error) {
	u, data, etag, err := c.get(ctx, id)
	if err != nil {
		return Event{}, err
	}
	evs, err := eventsFrom(data, c.loc)
	if err != nil {
		return Event{}, err
	}
	if len(evs) != 1 {
		return Event{}, fmt.Errorf("delete the event: it holds %d events, and only a single event can be deleted", len(evs))
	}
	if evs[0].Recurring {
		return Event{}, errors.New("delete the event: it repeats, and deleting a repeating event isn't supported yet; delete it in Calendar")
	}
	if _, _, err := c.do(ctx, "DELETE", u, map[string]string{"If-Match": etag}, ""); err != nil {
		return Event{}, fmt.Errorf("delete the event: %w", err)
	}
	ev := evs[0]
	ev.ID = u.Path
	ev.Start, ev.End = ev.Start.In(c.loc), ev.End.In(c.loc)

	return ev, nil
}

// newUID makes a random UID, which also names the event's resource
func newUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	h := hex.EncodeToString(b[:])

	return strings.ToUpper(h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]), nil
}
