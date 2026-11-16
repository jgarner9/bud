package calendar

import (
	"strings"
	"testing"
	"time"
)

var chicago = mustZone("America/Chicago")

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}

	return l
}

const sample = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Apple Inc.//iCloud//EN\r\n" +
	"BEGIN:VTIMEZONE\r\nTZID:America/Chicago\r\nEND:VTIMEZONE\r\n" +
	"BEGIN:VEVENT\r\nUID:abc\r\nDTSTAMP:20261101T000000Z\r\n" +
	"DTSTART;TZID=America/Chicago:20261117T090000\r\nDTEND;TZID=America/Chicago:20261117T093000\r\n" +
	"SUMMARY:Dentist\\, then lunch\r\nLOCATION:Main St\r\nDESCRIPTION:line one\\nline two that is long enough to need fol\r\n ding at seventy-five octets\r\n" +
	"SEQUENCE:2\r\nX-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nTRIGGER:-PT15M\r\nDESCRIPTION:Reminder\r\nEND:VALARM\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

func TestEventsFromReadsAnAppleEvent(t *testing.T) {
	evs, err := eventsFrom(sample, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("%d events", len(evs))
	}
	e := evs[0]
	if e.Title != "Dentist, then lunch" || e.Location != "Main St" {
		t.Errorf("title %q location %q", e.Title, e.Location)
	}
	if e.Notes != "line one\nline two that is long enough to need folding at seventy-five octets" {
		t.Errorf("notes %q", e.Notes)
	}
	want := time.Date(2026, 11, 17, 9, 0, 0, 0, chicago)
	if !e.Start.Equal(want) || e.End.Sub(e.Start) != 30*time.Minute || e.AllDay {
		t.Errorf("start %v end %v all day %v", e.Start, e.End, e.AllDay)
	}
	if len(e.Alarms) != 1 || e.Alarms[0] != 15 {
		t.Errorf("alarms %v", e.Alarms)
	}
}

func TestEventsFromTimes(t *testing.T) {
	cases := []struct {
		name, lines    string
		start, end     time.Time
		allDay, repeat bool
	}{
		{"all day", "DTSTART;VALUE=DATE:20261120\r\n", time.Date(2026, 11, 20, 0, 0, 0, 0, chicago), time.Date(2026, 11, 21, 0, 0, 0, 0, chicago), true, false},
		{"utc and duration", "DTSTART:20261120T150000Z\r\nDURATION:PT1H30M\r\n", time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 20, 16, 30, 0, 0, time.UTC), false, false},
		{"floating", "DTSTART:20261120T080000\r\nDTEND:20261120T090000\r\n", time.Date(2026, 11, 20, 8, 0, 0, 0, chicago), time.Date(2026, 11, 20, 9, 0, 0, 0, chicago), false, false},
		{"unknown zone", "DTSTART;TZID=Custom Zone:20261120T080000\r\n", time.Date(2026, 11, 20, 8, 0, 0, 0, chicago), time.Date(2026, 11, 20, 8, 0, 0, 0, chicago), false, false},
		{"occurrence", "RECURRENCE-ID:20261120T150000Z\r\nDTSTART:20261120T150000Z\r\nDTEND:20261120T160000Z\r\n", time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC), false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:x\r\n" + c.lines + "END:VEVENT\r\nEND:VCALENDAR\r\n"
			evs, err := eventsFrom(data, chicago)
			if err != nil {
				t.Fatal(err)
			}
			e := evs[0]
			if !e.Start.Equal(c.start) || !e.End.Equal(c.end) || e.AllDay != c.allDay || e.Recurring != c.repeat {
				t.Errorf("got %v to %v all day %v recurring %v", e.Start, e.End, e.AllDay, e.Recurring)
			}
		})
	}
}

func TestParseICSRefusesBrokenObjects(t *testing.T) {
	for _, bad := range []string{
		"",
		"BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nEND:VCALENDAR\r\n",
		"BEGIN:VCALENDAR\r\nSUMMARY\r\nEND:VCALENDAR\r\n",
		"SUMMARY:x\r\n",
	} {
		if _, err := parseICS(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	if _, err := eventsFrom("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:x\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n", time.UTC); err == nil {
		t.Error("an event without DTSTART was accepted")
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"-PT15M": -15 * time.Minute, "P1D": 24 * time.Hour, "P1DT2H": 26 * time.Hour, "PT0S": 0, "P1W": 7 * 24 * time.Hour, "+PT1H": time.Hour,
	} {
		if got, err := parseDuration(in); err != nil || got != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "P", "PT", "15M", "PT15", "PTXM", "P1H"} {
		if _, err := parseDuration(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// a built event reads back as what went in, with its text escaped and its
// lines folded
func TestBuildEventRoundTrips(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	in := NewEvent{
		Title: "Call the plumber; bring the invoice, the old one", Start: time.Date(2026, 11, 18, 14, 30, 0, 0, chicago),
		End: time.Date(2026, 11, 18, 15, 0, 0, 0, chicago), Notes: strings.Repeat("é", 60) + "\nsecond line", Alarms: []int{0, 30},
	}
	data := buildEvent("UID-1", in, now)
	for _, line := range strings.Split(data, "\r\n") {
		if len(line) > 75 {
			t.Errorf("line over 75 octets: %q", line)
		}
	}
	evs, err := eventsFrom(data, chicago)
	if err != nil {
		t.Fatal(err)
	}
	e := evs[0]
	if e.Title != in.Title || e.Notes != in.Notes || !e.Start.Equal(in.Start) || !e.End.Equal(in.End) || len(e.Alarms) != 2 || e.Alarms[1] != 30 {
		t.Errorf("read back %+v", e)
	}

	allDay := buildEvent("UID-2", NewEvent{Title: "Trip", Start: time.Date(2026, 12, 1, 0, 0, 0, 0, chicago), End: time.Date(2026, 12, 4, 0, 0, 0, 0, chicago), AllDay: true}, now)
	if !strings.Contains(allDay, "DTSTART;VALUE=DATE:20261201\r\n") || !strings.Contains(allDay, "DTEND;VALUE=DATE:20261204\r\n") {
		t.Errorf("all-day event:\n%s", allDay)
	}
}

// moving keeps every line bud doesn't change, the alarms among them, and
// bumps SEQUENCE
func TestMoveEventKeepsTheRest(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	start := time.Date(2026, 11, 18, 10, 0, 0, 0, chicago)
	out, err := moveEvent(sample, start, start.Add(time.Hour), false, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC", "TRIGGER:-PT15M", "DESCRIPTION:Reminder", "SEQUENCE:3", "DTSTART:20261118T160000Z", "DTEND:20261118T170000Z", "BEGIN:VTIMEZONE"} {
		if !strings.Contains(out, want+"\r\n") {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Count(out, "DTSTART") != 1 || strings.Contains(out, "SEQUENCE:2") {
		t.Errorf("old times or sequence kept:\n%s", out)
	}
	evs, err := eventsFrom(out, chicago)
	if err != nil || !evs[0].Start.Equal(start) || evs[0].Title != "Dentist, then lunch" {
		t.Errorf("read back %+v %v", evs, err)
	}

	repeating := strings.Replace(sample, "SEQUENCE:2\r\n", "RRULE:FREQ=WEEKLY\r\n", 1)
	if _, err := moveEvent(repeating, start, start.Add(time.Hour), false, now); err == nil || !strings.Contains(err.Error(), "repeat") {
		t.Errorf("a repeating event: err = %v", err)
	}
}
