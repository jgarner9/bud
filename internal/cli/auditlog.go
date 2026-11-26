package cli

import (
	"jgarner.dev/bud/internal/audit"
	"jgarner.dev/bud/internal/paths"
)

// openAudit opens the shared audit log (Suite D140) for a leaf. The log is a
// record and nothing decides from it, so one that won't open is reported and
// the leaf runs without it rather than going down
func openAudit(g *Globals, p *paths.Paths) *audit.Log {
	l, err := audit.Open(p.Audit())
	if err != nil {
		g.Noticef("audit: the shared audit log is off (%v); fix it and restart", err)
		return nil
	}
	return l
}
