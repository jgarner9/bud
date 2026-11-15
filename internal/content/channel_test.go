package content

import (
	"strings"
	"testing"
)

func TestNewChannel(t *testing.T) {
	for name, tc := range map[string]struct {
		edge, stable string
		want         string // the error, or "" for none
	}{
		"edge only":            {"2026.11.15.1", "", ""},
		"stable at edge":       {"2026.11.15.2", "2026.11.15.2", ""},
		"stable behind edge":   {"2026.11.15.2", "2026.11.14.1", ""},
		"stable ahead of edge": {"2026.11.15.1", "2026.11.15.2", "ahead of edge"},
		"no edge":              {"", "", "edge"},
		"a bad edge":           {"2026.11.15", "", "isn't a content version"},
		"a bad stable":         {"2026.11.15.1", "2026.13.01.1", "stable"},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := NewChannel(tc.edge, tc.stable)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want error %q, got %v", tc.want, err)
			}
			if err == nil && (c.Channels["edge"].Content != tc.edge || c.Channels["stable"].Content != tc.stable) {
				t.Fatalf("got %+v", c)
			}
		})
	}
}

func TestMarshalChannelRoundTrips(t *testing.T) {
	s := newSigner(t)
	trust(t, s)
	c, err := NewChannel("2026.11.15.2", "2026.11.15.1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := MarshalChannel(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyChannel(b, s.sign(b, ChannelNamespace))
	if err != nil || got.Channels["edge"].Content != "2026.11.15.2" || got.Channels["stable"].Content != "2026.11.15.1" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if !strings.Contains(string(b), "\"edge\": {\n      \"content\": \"2026.11.15.2\"\n    }") {
		t.Fatalf("not indented as expected:\n%s", b)
	}
}
