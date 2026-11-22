package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"jgarner.dev/bud/internal/mcpgateway"
)

// checkGatewayTrust fails when Hermes trusts a gateway server that the
// gateway doesn't review (Suite D115). bud sync renders Linear, the
// calendar and mail trusted in Hermes because the gateway asks for their
// writes; if the gateway serves that server without reviewing it (it isn't
// in the gateway's config, or the config is broken), a write would run with
// nobody asked. The gateway itself refuses a server with neither an approval
// block nor read_only (D103). The tiers file names the servers
// Hermes trusts, and the gateway's config (with the approvals file bud sync
// lays over it) says which it reviews. A server declared read_only counts as
// reviewed, since the gateway takes that as every tool only reading
func checkGatewayTrust(tiersPath, gatewayPath string) Result {
	r := Result{Name: "gateway approvals"}
	b, err := os.ReadFile(tiersPath)
	if errors.Is(err, fs.ErrNotExist) {
		r.Status, r.Detail, r.Fix = Warn, "no Hermes tiers file at "+tiersPath+", so nothing says which servers Hermes trusts", "bud sync"
		return r
	}
	// Trusted is a pointer so a file from before D115, which has no
	// trusted key, isn't read as trusting nothing
	var t struct {
		Trusted *[]string `json:"trusted"`
	}
	if err == nil {
		err = json.Unmarshal(b, &t)
	}
	if err != nil {
		r.Status, r.Detail, r.Fix = Fail, fmt.Sprintf("can't read %s: %v", tiersPath, err), "bud sync"
		return r
	}
	if t.Trusted == nil {
		// a warning, not a failure: right after bud update and before the
		// sync that follows it, the file is the older bud's, and a failure
		// here would roll the update back
		r.Status = Warn
		r.Detail = tiersPath + " is from a bud before D115, so it doesn't say which gateway servers Hermes trusts and they weren't checked"
		r.Fix = "bud sync"
		return r
	}
	trusted := *t.Trusted
	if len(trusted) == 0 {
		r.Detail = "Hermes trusts no gateway server, so its own prompt covers every gateway write"
		return r
	}
	cfg, err := mcpgateway.Read(gatewayPath)
	if err != nil {
		r.Status = Fail
		r.Detail = fmt.Sprintf("Hermes trusts %s to the gateway's review, and the gateway's config can't be read (%v)", strings.Join(trusted, ", "), err)
		r.Fix = "fix the gateway's config (docs/internals/mcp-gateway.md), then bud doctor again"
		return r
	}
	var unreviewed []string
	for _, name := range trusted {
		s, ok := cfg.Servers[name]
		if !ok || (s.Approval == nil && !s.ReadOnly) {
			unreviewed = append(unreviewed, name)
		}
	}
	if len(unreviewed) > 0 {
		r.Status = Fail
		r.Detail = fmt.Sprintf("Hermes doesn't ask before writes to %s, because the gateway should, and the gateway's config doesn't review them, so a write would run unasked", strings.Join(unreviewed, ", "))
		r.Fix = "give each an approval block in " + gatewayPath + " (or drop its own one so bud sync's applies), or mark it trust: untrusted in your overlay's bud.hermes.mcp_servers"
		return r
	}
	r.Detail = fmt.Sprintf("the gateway reviews every server Hermes trusts to it: %s", strings.Join(trusted, ", "))

	return r
}
