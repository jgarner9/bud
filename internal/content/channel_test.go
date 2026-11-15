package content

import (
	"strings"
	"testing"
)

func TestSetContent(t *testing.T) {
	at := func(edge, stable string) *Channel {
		c := &Channel{Channels: map[string]Versions{}}
		if edge != "" {
			c.Channels["edge"] = Versions{Content: edge, Code: "1.2.0"}
		}
		if stable != "" {
			c.Channels["stable"] = Versions{Content: stable}
		}
		return c
	}
	for name, tc := range map[string]struct {
		from             *Channel
		channel, version string
		changed          bool
		want             string // the error, or "" for none
	}{
		"the first edge release":       {&Channel{}, "edge", "2026.11.15.1", true, ""},
		"a newer edge release":         {at("2026.11.15.1", ""), "edge", "2026.11.15.2", true, ""},
		"the same edge release again":  {at("2026.11.15.1", ""), "edge", "2026.11.15.1", false, ""},
		"an older edge release":        {at("2026.11.15.2", ""), "edge", "2026.11.15.1", false, "only moves forward"},
		"promoting what edge has":      {at("2026.11.15.2", ""), "stable", "2026.11.15.2", true, ""},
		"promoting an older edge one":  {at("2026.11.15.2", "2026.11.14.1"), "stable", "2026.11.15.1", true, ""},
		"promoting past edge":          {at("2026.11.15.1", ""), "stable", "2026.11.15.2", false, "edge is at"},
		"promoting with no edge":       {&Channel{}, "stable", "2026.11.15.1", false, "edge is at"},
		"moving stable back":           {at("2026.11.15.2", "2026.11.15.2"), "stable", "2026.11.15.1", false, "only moves forward"},
		"a channel that doesn't exist": {&Channel{}, "beta", "2026.11.15.1", false, "isn't a channel"},
		"a bad version":                {&Channel{}, "edge", "2026.11.15", false, "isn't a content version"},
	} {
		t.Run(name, func(t *testing.T) {
			changed, err := SetContent(tc.from, tc.channel, tc.version)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want error %q, got %v", tc.want, err)
			}
			if changed != tc.changed {
				t.Fatalf("changed = %v, want %v", changed, tc.changed)
			}
			if err == nil && tc.from.Channels[tc.channel].Content != tc.version {
				t.Fatalf("%s is at %q, want %q", tc.channel, tc.from.Channels[tc.channel].Content, tc.version)
			}
		})
	}
}

func TestSetContentKeepsTheCodeVersion(t *testing.T) {
	c := &Channel{Channels: map[string]Versions{"edge": {Content: "2026.11.15.1", Code: "1.2.0"}}}
	if _, err := SetContent(c, "edge", "2026.11.15.2"); err != nil {
		t.Fatal(err)
	}
	if c.Channels["edge"].Code != "1.2.0" {
		t.Fatalf("code version lost: %+v", c.Channels["edge"])
	}
}

func TestMarshalChannelRoundTrips(t *testing.T) {
	s := newSigner(t)
	trust(t, s)
	c := &Channel{}
	if _, err := SetContent(c, "edge", "2026.11.15.1"); err != nil {
		t.Fatal(err)
	}
	b, err := MarshalChannel(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyChannel(b, s.sign(b, ChannelNamespace))
	if err != nil || got.Channels["edge"].Content != "2026.11.15.1" {
		t.Fatalf("got %+v, %v", got, err)
	}
	empty, err := MarshalChannel(&Channel{})
	if err != nil || string(empty) != "{\n  \"channels\": {}\n}\n" {
		t.Fatalf("an empty channel.json: %q, %v", empty, err)
	}
}
