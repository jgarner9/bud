// Package tailnet says which addresses are inside the house: loopback and
// Tailscale's ranges. The home server's services (the MCP gateway, the
// router) bind only to such addresses (Suite D72)
package tailnet

import "net/netip"

// ranges holds Tailscale's address ranges: the CGNAT block it gives IPv4
// addresses from, and its IPv6 ULA prefix
var ranges = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

// Inside says whether host is an IP address on loopback or the tailnet.
// "localhost" counts for an upstream URL, though not for a listen address,
// where a resolver could map it elsewhere; callers that bind refuse it
// themselves
func Inside(host string) bool {
	if host == "localhost" {
		return true
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return InsideAddr(a)
}

// InsideAddr says whether a is on loopback or the tailnet
func InsideAddr(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() {
		return true
	}
	for _, p := range ranges {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
