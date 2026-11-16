package calendar

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// prop is one iCalendar content line (RFC 5545 3.1), unfolded
type prop struct {
	Name   string            // upper case, such as DTSTART
	Params map[string]string // upper-case names, values unquoted
	Value  string            // still escaped
}

// component is one BEGIN/END block and what it holds
type component struct {
	Name  string
	Props []prop
	Comps []*component
}

func (c *component) get(name string) (prop, bool) {
	for _, p := range c.Props {
		if p.Name == name {
			return p, true
		}
	}

	return prop{}, false
}

// unfold joins folded lines (a line break followed by a space or tab) and
// splits the result into lines, accepting CRLF or bare LF
func unfold(data string) []string {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\n ", "")
	data = strings.ReplaceAll(data, "\n\t", "")
	var lines []string
	for l := range strings.SplitSeq(data, "\n") {
		if l = strings.TrimRight(l, "\r"); l != "" {
			lines = append(lines, l)
		}
	}

	return lines
}

// parseLine splits NAME;PARAM=a;Q="b,c":value
func parseLine(line string) (prop, error) {
	p := prop{Params: map[string]string{}}
	i, inQuote := 0, false
	// the name and parameters end at the first colon outside quotes
	for ; i < len(line); i++ {
		if line[i] == '"' {
			inQuote = !inQuote
		}
		if line[i] == ':' && !inQuote {
			break
		}
	}
	if i == len(line) {
		return p, fmt.Errorf("content line %q has no value", line)
	}
	head, value := line[:i], line[i+1:]
	parts := splitParams(head)
	p.Name = strings.ToUpper(parts[0])
	for _, kv := range parts[1:] {
		k, v, _ := strings.Cut(kv, "=")
		p.Params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	p.Value = value

	return p, nil
}

// splitParams splits on semicolons outside double quotes
func splitParams(s string) []string {
	var out []string
	start, inQuote := 0, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case ';':
			if !inQuote {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}

	return append(out, s[start:])
}

// parseICS reads an iCalendar object into its top component (VCALENDAR)
func parseICS(data string) (*component, error) {
	var stack []*component
	var root *component
	for _, line := range unfold(data) {
		p, err := parseLine(line)
		if err != nil {
			return nil, err
		}
		switch p.Name {
		case "BEGIN":
			c := &component{Name: strings.ToUpper(p.Value)}
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.Comps = append(top.Comps, c)
			} else if root == nil {
				root = c
			}
			stack = append(stack, c)
		case "END":
			if len(stack) == 0 || stack[len(stack)-1].Name != strings.ToUpper(p.Value) {
				return nil, fmt.Errorf("END:%s doesn't close an open component", p.Value)
			}
			stack = stack[:len(stack)-1]
		default:
			if len(stack) == 0 {
				return nil, fmt.Errorf("content line %s is outside any component", p.Name)
			}
			top := stack[len(stack)-1]
			top.Props = append(top.Props, p)
		}
	}
	if root == nil || len(stack) > 0 {
		return nil, fmt.Errorf("not a complete iCalendar object")
	}

	return root, nil
}

// unescapeText undoes RFC 5545 3.3.11 TEXT escaping
func unescapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}

	return b.String()
}

// escapeText escapes a TEXT value
func escapeText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`)

	return r.Replace(s)
}

// fold writes a content line, folded at 75 octets without splitting a
// UTF-8 sequence, ending in CRLF
func fold(b *strings.Builder, line string) {
	for len(line) > 75 {
		cut := 75
		for cut > 0 && line[cut]&0xC0 == 0x80 {
			cut--
		}
		b.WriteString(line[:cut] + "\r\n ")
		line = line[cut:]
	}
	b.WriteString(line + "\r\n")
}

// parseTime reads a DATE or DATE-TIME property. A date is all day. A UTC
// time ends in Z; one with TZID is in that zone, falling back to loc when Go
// doesn't know the zone; a floating time is in loc
func parseTime(p prop, loc *time.Location) (t time.Time, allDay bool, err error) {
	v := p.Value
	if p.Params["VALUE"] == "DATE" || len(v) == 8 {
		t, err = time.ParseInLocation("20060102", v, loc)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.Parse("20060102T150405Z", v)
		return t, false, err
	}
	in := loc
	if tz := p.Params["TZID"]; tz != "" {
		if l, lerr := time.LoadLocation(strings.TrimPrefix(tz, "/")); lerr == nil {
			in = l
		}
	}
	t, err = time.ParseInLocation("20060102T150405", v, in)

	return t, false, err
}

// parseDuration reads an RFC 5545 3.3.6 duration such as -PT15M or P1DT2H
func parseDuration(s string) (time.Duration, error) {
	orig := s
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg, s = true, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	if !strings.HasPrefix(s, "P") || len(s) < 3 {
		return 0, fmt.Errorf("%q isn't a duration", orig)
	}
	s = s[1:]
	var d time.Duration
	inTime := false
	num := ""
	for _, r := range s {
		switch {
		case r == 'T':
			inTime = true
		case r >= '0' && r <= '9':
			num += string(r)
		default:
			n, err := strconv.Atoi(num)
			if err != nil {
				return 0, fmt.Errorf("%q isn't a duration", orig)
			}
			num = ""
			unit := map[rune]time.Duration{'W': 7 * 24 * time.Hour, 'D': 24 * time.Hour}
			if inTime {
				unit = map[rune]time.Duration{'H': time.Hour, 'M': time.Minute, 'S': time.Second}
			}
			u, ok := unit[r]
			if !ok {
				return 0, fmt.Errorf("%q isn't a duration", orig)
			}
			d += time.Duration(n) * u
		}
	}
	if num != "" {
		return 0, fmt.Errorf("%q isn't a duration", orig)
	}
	if neg {
		d = -d
	}

	return d, nil
}

// Event is one event, or one occurrence of a repeating one
type Event struct {
	ID       string    `json:"id"` // the resource's path, which moving or deleting it takes
	Calendar string    `json:"calendar"`
	Title    string    `json:"title"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	AllDay   bool      `json:"all_day,omitempty"`
	Location string    `json:"location,omitempty"`
	Notes    string    `json:"notes,omitempty"`
	// Alarms are minutes before the start at which the phone alerts
	Alarms []int `json:"alarm_minutes,omitempty"`
	// Repeats is the repeat rule of an event the server didn't expand
	Repeats string `json:"repeats,omitempty"`
	// Recurring marks an occurrence of a repeating event, which can't be
	// moved or deleted one at a time here
	Recurring bool `json:"recurring,omitempty"`
}

// eventsFrom reads the VEVENTs in one calendar object
func eventsFrom(data string, loc *time.Location) ([]Event, error) {
	root, err := parseICS(data)
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, c := range root.Comps {
		if c.Name != "VEVENT" {
			continue
		}
		e, err := eventFrom(c, loc)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}

	return out, nil
}

func eventFrom(c *component, loc *time.Location) (Event, error) {
	var e Event
	if p, ok := c.get("SUMMARY"); ok {
		e.Title = unescapeText(p.Value)
	}
	if p, ok := c.get("LOCATION"); ok {
		e.Location = unescapeText(p.Value)
	}
	if p, ok := c.get("DESCRIPTION"); ok {
		e.Notes = unescapeText(p.Value)
	}
	p, ok := c.get("DTSTART")
	if !ok {
		return e, fmt.Errorf("event %q has no DTSTART", e.Title)
	}
	start, allDay, err := parseTime(p, loc)
	if err != nil {
		return e, fmt.Errorf("event %q: DTSTART: %w", e.Title, err)
	}
	e.Start, e.AllDay = start, allDay
	switch {
	case has(c, "DTEND"):
		p, _ := c.get("DTEND")
		if e.End, _, err = parseTime(p, loc); err != nil {
			return e, fmt.Errorf("event %q: DTEND: %w", e.Title, err)
		}
	case has(c, "DURATION"):
		p, _ := c.get("DURATION")
		d, err := parseDuration(p.Value)
		if err != nil {
			return e, fmt.Errorf("event %q: DURATION: %w", e.Title, err)
		}
		e.End = e.Start.Add(d)
	case allDay:
		e.End = e.Start.AddDate(0, 0, 1)
	default:
		e.End = e.Start
	}
	if p, ok := c.get("RRULE"); ok {
		e.Repeats, e.Recurring = p.Value, true
	}
	if has(c, "RECURRENCE-ID") {
		e.Recurring = true
	}
	for _, a := range c.Comps {
		if a.Name != "VALARM" {
			continue
		}
		t, ok := a.get("TRIGGER")
		if !ok || t.Params["VALUE"] == "DATE-TIME" {
			continue
		}
		if d, err := parseDuration(t.Value); err == nil && d <= 0 {
			e.Alarms = append(e.Alarms, int(-d/time.Minute))
		}
	}

	return e, nil
}

func has(c *component, name string) bool {
	_, ok := c.get(name)
	return ok
}

// NewEvent is what creating an event takes
type NewEvent struct {
	Title    string
	Start    time.Time
	End      time.Time
	AllDay   bool
	Location string
	Notes    string
	Alarms   []int // minutes before the start
}

// prodID names bud as the event's author
const prodID = "-//jgarner.dev//bud calendar-mcp//EN"

// buildEvent writes a calendar object holding one event. Timed events are
// written in UTC, which every client shows in its own zone
func buildEvent(uid string, e NewEvent, now time.Time) string {
	var b strings.Builder
	fold(&b, "BEGIN:VCALENDAR")
	fold(&b, "VERSION:2.0")
	fold(&b, "PRODID:"+prodID)
	fold(&b, "BEGIN:VEVENT")
	fold(&b, "UID:"+uid)
	fold(&b, "DTSTAMP:"+utc(now))
	if e.AllDay {
		fold(&b, "DTSTART;VALUE=DATE:"+e.Start.Format("20060102"))
		fold(&b, "DTEND;VALUE=DATE:"+e.End.Format("20060102"))
	} else {
		fold(&b, "DTSTART:"+utc(e.Start))
		fold(&b, "DTEND:"+utc(e.End))
	}
	fold(&b, "SUMMARY:"+escapeText(e.Title))
	if e.Location != "" {
		fold(&b, "LOCATION:"+escapeText(e.Location))
	}
	if e.Notes != "" {
		fold(&b, "DESCRIPTION:"+escapeText(e.Notes))
	}
	for _, m := range e.Alarms {
		fold(&b, "BEGIN:VALARM")
		fold(&b, "ACTION:DISPLAY")
		fold(&b, "DESCRIPTION:"+escapeText(e.Title))
		fold(&b, fmt.Sprintf("TRIGGER:-PT%dM", m))
		fold(&b, "END:VALARM")
	}
	fold(&b, "END:VEVENT")
	fold(&b, "END:VCALENDAR")

	return b.String()
}

func utc(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// moveEvent rewrites a calendar object holding one event that doesn't
// repeat, giving it a new start and end. Every other line is kept as it
// was, so properties bud doesn't know survive; SEQUENCE goes up by one, as
// RFC 5545 asks when an event's time changes
func moveEvent(data string, start, end time.Time, allDay bool, now time.Time) (string, error) {
	root, err := parseICS(data)
	if err != nil {
		return "", err
	}
	events := 0
	for _, c := range root.Comps {
		if c.Name == "VEVENT" {
			events++
			if has(c, "RRULE") || has(c, "RECURRENCE-ID") || has(c, "RDATE") {
				return "", fmt.Errorf("it repeats, and moving a repeating event isn't supported yet; move it in Calendar")
			}
		}
	}
	if events != 1 {
		return "", fmt.Errorf("it holds %d events, and only a single event can be moved", events)
	}

	var b strings.Builder
	var stack []string
	seq := 0
	for _, line := range unfold(data) {
		p, err := parseLine(line)
		if err != nil {
			return "", err
		}
		top := ""
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch {
		case p.Name == "BEGIN":
			stack = append(stack, strings.ToUpper(p.Value))
		case p.Name == "END":
			if top == "VEVENT" {
				// the new times and sequence go last, in place of the dropped ones
				if allDay {
					fold(&b, "DTSTART;VALUE=DATE:"+start.Format("20060102"))
					fold(&b, "DTEND;VALUE=DATE:"+end.Format("20060102"))
				} else {
					fold(&b, "DTSTART:"+utc(start))
					fold(&b, "DTEND:"+utc(end))
				}
				fold(&b, "DTSTAMP:"+utc(now))
				fold(&b, "LAST-MODIFIED:"+utc(now))
				fold(&b, fmt.Sprintf("SEQUENCE:%d", seq+1))
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case top == "VEVENT":
			// only the event's own properties change, not its alarms'
			switch p.Name {
			case "DTSTART", "DTEND", "DURATION", "DTSTAMP", "LAST-MODIFIED":
				continue
			case "SEQUENCE":
				seq, _ = strconv.Atoi(p.Value)
				continue
			}
		}
		fold(&b, line)
	}

	return b.String(), nil
}
