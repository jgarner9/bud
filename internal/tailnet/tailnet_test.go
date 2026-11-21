package tailnet

import "testing"

func TestInsideAcceptsOnlyLoopbackAndTailnetAddresses(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1":           true,
		"::1":                 true,
		"::ffff:127.0.0.1":    true,
		"100.64.0.9":          true,
		"100.127.255.255":     true,
		"fd7a:115c:a1e0::1":   true,
		"localhost":           true,
		"100.128.0.1":         false,
		"192.168.1.5":         false,
		"0.0.0.0":             false,
		"":                    false,
		"home.tailnet.ts.net": false,
	} {
		if got := Inside(host); got != want {
			t.Errorf("Inside(%q) = %v, want %v", host, got, want)
		}
	}
}
