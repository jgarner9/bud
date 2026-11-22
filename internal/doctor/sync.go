package doctor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"jgarner.dev/bud/internal/agent"
	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/content"
	"jgarner.dev/bud/internal/overlay"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/profile"
	"jgarner.dev/bud/internal/registry"
	"jgarner.dev/bud/internal/syncer"
)

// The drift checks (build plan task 1.9, Suite D46): what bud sync left on
// this device against its ledger, what this bud would render now, and with
// --net whether the channel has moved on and the registry's endpoints
// answer (--net gates only these; the ai backend check has its own rules). A device with no profile set has never been synced, so it gets
// none of them

// swappable in tests
var (
	syncCheck    = syncer.Check
	fetchChannel = syncer.FetchChannel
	netClient    = &http.Client{Timeout: 10 * time.Second}
)

// netTimeout bounds every --net check together
const netTimeout = 30 * time.Second

func checkSync(p *paths.Paths, cfg config.Settings, opts Options) []Result {
	if cfg.Device == "" {
		return nil
	}
	// a sync switches the links before saving the ledger, so read only
	// while none runs, or a healthy sync could look broken and roll back
	// a bud update
	unlock, err := syncer.TryLock(p)
	if errors.Is(err, syncer.ErrBusy) {
		return []Result{{Name: "sync", Status: Warn, Detail: "a bud sync is running, so its checks were skipped", Fix: "bud doctor again once it's done"}}
	}
	if err != nil {
		return []Result{{Name: "sync", Status: Warn, Detail: "couldn't take the sync lock, so its checks were skipped: " + err.Error()}}
	}
	defer unlock()
	repo := syncer.OverlayRepo(cfg.Overlay.Repo)
	overlayDir := syncer.OverlayOnDisk(p, repo)

	prof, profResult := checkDevice(p, cfg.Device)
	results := []Result{profResult}
	if prof != nil && prof.Has("server") {
		results = append(results, checkReviewChannel(p), checkSignalDaemon(), checkGatewayTrust(p.HermesTiers(), p.MCPGateway()))
		if !opts.Quick {
			results = append(results, checkHermesVersion())
		}
	}
	if !opts.Quick && prof != nil && slices.Contains(prof.Components, "harness") {
		results = append(results, checkOpenCodeHarness())
	}
	led, ledResult := checkLedger(p)
	results = append(results, ledResult)
	if led == nil || led.Content == "" || ledResult.Status == Fail {
		return results
	}

	reg, regResult := checkRegistry(filepath.Join(p.Content(), led.Content), overlayDir)
	results = append(results, regResult)
	if prof != nil {
		results = append(results, checkLayers(filepath.Join(p.Content(), led.Content), repo, overlayDir, prof, led))
	}
	if prof != nil && reg != nil {
		results = append(results, checkRender(p, cfg, repo, led))
	}
	if !opts.Net {
		return results
	}

	ctx, cancel := opts.netContext()
	defer cancel()
	if prof != nil {
		results = append(results, checkChannel(ctx, cfg.Content.Mirror, prof, led))
	}
	if reg != nil {
		results = append(results, checkEndpoints(ctx, reg))
		results = append(results, checkEmbeddings(ctx, reg)...)
	}

	return results
}

// checkReviewChannel looks for the approval reviewer's socket and secret
// (Suite D97, D103), which the bud-signal plugin creates and the gateway and
// ops dial. Only a server-profile device runs them, so only it is asked. A
// miss warns and never fails: without the reviewer, approval-tier calls are
// refused, which is safe, but nothing can be approved until it runs. It
// looks at files and doesn't dial, so no prompt reaches Signal
func checkReviewChannel(p *paths.Paths) Result {
	r := Result{Name: "approval reviewer"}
	var missing []string
	sock := p.ReviewSocket()
	if sock == "" {
		missing = append(missing, "no runtime dir (XDG_RUNTIME_DIR is unset), so there is no socket")
	} else if fi, err := os.Lstat(sock); err != nil || fi.Mode()&os.ModeSocket == 0 {
		missing = append(missing, "no socket at "+sock)
	}
	if err := approval.CheckSecret(p.ReviewSecret()); err != nil {
		missing = append(missing, "the secret isn't usable ("+err.Error()+")")
	}
	if len(missing) == 0 {
		r.Detail = "the reviewer's socket and secret are in place"
		return r
	}
	r.Status = Warn
	r.Detail = strings.Join(missing, "; ") + "; gateway and ops calls that need approval are refused until the bud-signal plugin is running"
	r.Fix = "restart Hermes's gateway so the bud-signal plugin starts its reviewer (docs/internals/signal.md)"

	return r
}

func checkOpenCodeHarness() Result {
	status := agent.CheckOpenCodeHarness()
	if len(status.Problems) != 0 {
		problem := status.Problems[0]
		return Result{Name: "OpenCode harness", Status: Warn, Detail: problem.What, Fix: problem.Fix}
	}
	return Result{Name: "OpenCode harness", Detail: status.Version}
}

// checkDevice finds and validates the device's profile, the overlay's first
func checkDevice(p *paths.Paths, device string) (*profile.Profile, Result) {
	r := Result{Name: "device profile"}
	prof, file, err := profile.Find(device, syncer.ProfileDirs(p)...)
	if err != nil {
		r.Status = Fail
		r.Detail = err.Error()
		r.Fix = "fix the profile, or pick another with bud config set device <name>"
		return nil, r
	}
	r.Detail = fmt.Sprintf("%s on %s, from %s", prof.Name, prof.Channel, file)

	return prof, r
}

// checkLedger reads sync.json, checks the current link points where it
// says, and checks every file it records against its hash. Drift only
// warns: it's an edit on this device, which no bud update could cause or a
// rollback fix. A current link that's gone or moved fails, since OpenCode's
// instructions and skills point through it
func checkLedger(p *paths.Paths) (*syncer.Ledger, Result) {
	r := Result{Name: "sync ledger"}
	led, err := syncer.LoadLedger(p.SyncLedger())
	if err != nil {
		r.Status = Fail
		r.Detail = err.Error()
		return nil, r
	}
	if led.Content == "" {
		r.Status = Warn
		r.Detail = "this device hasn't synced yet"
		r.Fix = "bud sync"
		return led, r
	}

	dir := filepath.Join(p.Content(), led.Content)
	if cur := syncer.CurrentLink(p); cur != led.Content {
		r.Status = Fail
		if cur == "" {
			cur = "nothing"
		}
		r.Detail = fmt.Sprintf("%s points at %s, and the last sync left content %s", p.ContentCurrent(), cur, led.Content)
		r.Fix = "bud sync"
		return led, r
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		r.Status = Fail
		r.Detail = fmt.Sprintf("content %s is gone from %s", led.Content, dir)
		r.Fix = "bud sync (it fetches the version again)"
		return led, r
	}

	drift, err := led.Drift()
	if err != nil {
		r.Status = Fail
		r.Detail = err.Error()
		return led, r
	}
	if len(drift) == 0 {
		r.Detail = fmt.Sprintf("content %s, %s as bud sync wrote them", led.Content, plural(len(led.Files), "file"))
		return led, r
	}

	var missing, changed []string
	for _, d := range drift {
		if d.Missing {
			missing = append(missing, d.Path)
		} else {
			changed = append(changed, d.Path)
		}
	}
	r.Status = Warn
	var problems, fixes []string
	if len(missing) > 0 {
		problems = append(problems, "missing: "+strings.Join(missing, ", "))
		fixes = append(fixes, "bud sync writes missing files again")
	}
	if len(changed) > 0 {
		problems = append(problems, "changed by hand: "+strings.Join(changed, ", "))
		fixes = append(fixes, "bud sync leaves a hand edit alone, so put the change in the overlay, delete the file and run bud sync")
	}
	r.Detail = strings.Join(problems, "; ")
	r.Fix = strings.Join(fixes, "; ")

	return led, r
}

// checkRegistry validates the registry the last sync rendered from: the
// content's, with the overlay's merged over it
func checkRegistry(dir, overlayDir string) (*registry.Registry, Result) {
	r := Result{Name: "registry"}
	reg, err := syncer.LoadRegistry(dir, overlayDir)
	if err != nil {
		r.Status = Fail
		r.Detail = err.Error()
		r.Fix = "fix the overlay's env/registry.jsonc (bud registry check <file> says how), then bud sync"
		if overlayDir == "" {
			r.Fix = "bud sync to a content release with a valid registry"
		}
		return nil, r
	}
	r.Detail = fmt.Sprintf("%s, %s", plural(len(reg.Roles), "role"), plural(len(reg.Models), "model"))
	if overlayDir != "" {
		r.Detail += ", with the overlay's"
	}

	return reg, r
}

// checkLayers names every piece of content the device takes and the layer
// it comes from, the way the last sync layered them: the overlay's items
// (and which of them replace a public one), the public ones, and the files
// rendered from them. Items are read from the overlay as it is on disk,
// and a device that runs no OpenCode takes only skills and plugins, as the
// sync does. It only reports, so it never fails; an overlay that
// is set but not on disk warns, since the device runs on public content
// alone until a sync clones it
func checkLayers(dir, repo, overlayDir string, prof *profile.Profile, led *syncer.Ledger) Result {
	r := Result{Name: "content layers"}
	items, err := overlay.Layer(dir, overlayDir)
	if err != nil {
		r.Status = Warn
		r.Detail = "couldn't layer the content: " + err.Error()
		r.Fix = "bud sync"
		return r
	}

	opencode := prof.Has("harness") || prof.Has("server")
	byLayer := map[string][]string{}
	for _, it := range items {
		if it.Kind == "env" && it.ID == "registry" {
			continue // merged into the registry, never a fragment
		}
		if it.Kind != "instructions" && !prof.Selects(it.Kind, it.ID) {
			continue
		}
		if !opencode && it.Kind != "skills" && it.Kind != "plugins" {
			continue // only OpenCode reads the rest, and the sync drops it
		}
		name := it.Kind + "/" + it.ID
		if it.Overrides {
			name += " (replaces public)"
		}
		byLayer[it.Layer] = append(byLayer[it.Layer], name)
	}
	for _, f := range led.Files {
		if f.Layer == "rendered" {
			byLayer["rendered"] = append(byLayer["rendered"], filepath.Base(f.Path))
		}
	}

	var parts []string
	switch {
	case repo == "":
		parts = append(parts, "no overlay set, so public content only")
	case overlayDir == "":
		r.Status = Warn
		parts = append(parts, "the overlay "+repo+" isn't cloned yet, so public content only")
		r.Fix = "bud sync (it clones the overlay)"
	default:
		label := "overlay on disk"
		if led.Overlay != nil && led.Overlay.Commit != "" {
			label += " (last sync layered " + shortCommit(led.Overlay.Commit) + ")"
		}
		parts = append(parts, label+": "+listOrNone(byLayer[overlay.Private]))
	}
	parts = append(parts, "public: "+listOrNone(byLayer[overlay.Public]))
	if len(byLayer["rendered"]) > 0 {
		parts = append(parts, "rendered: "+strings.Join(byLayer["rendered"], ", "))
	}
	r.Detail = strings.Join(parts, "; ")

	return r
}

func listOrNone(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}

	return strings.Join(names, ", ")
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}

	return c
}

// checkRender re-renders the version in use, offline, and compares it with
// the disk. Files the ledger check already named are left out of it
func checkRender(p *paths.Paths, cfg config.Settings, repo string, led *syncer.Ledger) Result {
	r := Result{Name: "synced config"}
	rep, err := syncCheck(syncer.Options{
		Paths:    p,
		Device:   cfg.Device,
		Overlay:  repo,
		Profiles: func() []profile.Dir { return syncer.ProfileDirs(p) },
	})
	if err != nil {
		r.Status = Fail
		r.Detail = err.Error()
		r.Fix = "bud sync"
		return r
	}

	named := map[string]bool{}
	if drift, err := led.Drift(); err == nil {
		for _, d := range drift {
			named[d.Path] = true
		}
	}
	var changes, notOurs []string
	for _, c := range rep.Changes {
		if !named[c.Path] {
			changes = append(changes, c.Action+" "+c.Path)
		}
	}
	for _, c := range rep.Skipped {
		if !named[c.Path] {
			notOurs = append(notOurs, c.Path)
		}
	}
	if len(changes) == 0 && len(notOurs) == 0 {
		r.Detail = "matches what this bud renders from content " + led.Content
		return r
	}

	r.Status = Warn
	var problems, fixes []string
	if len(changes) > 0 {
		problems = append(problems, "bud sync would "+strings.Join(changes, ", "))
		fixes = append(fixes, "bud sync")
	}
	if len(notOurs) > 0 {
		problems = append(problems, "not bud's, so bud's version isn't in use: "+strings.Join(notOurs, ", "))
		fixes = append(fixes, "move "+strings.Join(notOurs, ", ")+" aside, then bud sync")
	}
	r.Detail = strings.Join(problems, "; ")
	r.Fix = strings.Join(fixes, "; ")

	return r
}

// checkChannel asks the release host which content the device's channel
// offers. Being behind only warns: a sync isn't due until the timer runs.
// Not reaching the host warns too, since offline isn't broken
func checkChannel(ctx context.Context, mirror string, prof *profile.Profile, led *syncer.Ledger) Result {
	r := Result{Name: "content channel", Status: Warn}
	ch, err := fetchChannel(ctx, mirror)
	if err != nil {
		r.Detail = "couldn't check: " + err.Error()
		return r
	}
	offered := ch.Channels[prof.Channel].Content
	switch {
	case offered == "":
		r.Detail = "channel.json offers no content on " + prof.Channel + " yet"
	case offered == led.Content:
		r.Status = OK
		r.Detail = fmt.Sprintf("content %s is the newest on %s", offered, prof.Channel)
	case led.HeldBack(offered):
		r.Status = OK
		r.Detail = fmt.Sprintf("held on %s after a rollback; %s offers %s", led.Content, prof.Channel, offered)
	case content.CheckNewer(led.Content, offered) == nil:
		r.Detail = fmt.Sprintf("content %s is out on %s; this device has %s", offered, prof.Channel, led.Content)
		r.Fix = "bud sync"
	default:
		r.Detail = fmt.Sprintf("%s offers %s, older than %s in use", prof.Channel, offered, led.Content)
	}

	return r
}

// checkEndpoints asks each provider a role uses for its model list. Any
// HTTP answer means it's up. A provider whose key is an {env:VAR} that's
// set gets the key, so its list says whether the registry's model names
// exist; an op:// key is never read (Suite D7), so those only show they
// answer. Down only warns: the Qwen box sleeps when idle (Suite D26)
func checkEndpoints(ctx context.Context, reg *registry.Registry) Result {
	r := Result{Name: "model endpoints"}
	names := map[string][]string{} // provider -> model names its roles use
	for _, role := range reg.Roles {
		for _, id := range role.Members() {
			m := reg.Models[id]
			if !slices.Contains(names[m.Provider], m.Name) {
				names[m.Provider] = append(names[m.Provider], m.Name)
			}
		}
	}
	ids := make([]string, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	var problems []string
	answered := 0
	for _, id := range ids {
		slices.Sort(names[id])
		if problem := probe(ctx, id, reg, names[id]); problem != "" {
			problems = append(problems, problem)
		} else {
			answered++
		}
	}
	if len(problems) == 0 {
		r.Detail = plural(answered, "provider") + " answered"
		return r
	}
	r.Status = Warn
	r.Detail = strings.Join(problems, "; ")
	r.Fix = "check the endpoints in the overlay's env/registry.jsonc; a local box may only be asleep"

	return r
}

// probe GETs the provider's /models and returns what's wrong, or empty.
// When the list carries facts (OpenCode Go's does), facts that drifted from
// the registry are reported too (Suite D69)
func probe(ctx context.Context, id string, reg *registry.Registry, models []string) string {
	prov := reg.Providers[id]
	base, err := url.Parse(prov.Endpoint)
	if err != nil {
		return fmt.Sprintf("%s: %v", id, err)
	}
	host := base.Host
	// onto the path, keeping any query
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.JoinPath("models").String(), nil)
	if err != nil {
		return fmt.Sprintf("%s: %v", id, err)
	}
	if v, ok := strings.CutPrefix(prov.Key, "{env:"); ok {
		if key := os.Getenv(strings.TrimSuffix(v, "}")); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	resp, err := netClient.Do(req)
	if err != nil {
		return fmt.Sprintf("%s (%s) didn't answer", id, host)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "" // up; without its key a provider won't say which models it has
	}

	list, err := registry.ParseCatalog(resp.Body)
	if err != nil {
		return "" // up, but not a list bud can read
	}
	listed := map[string]bool{}
	for _, m := range list {
		listed[m.ID] = true
	}
	var missing []string
	for _, name := range models {
		if !listed[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Sprintf("%s doesn't list %s", id, strings.Join(missing, ", "))
	}
	if drift := registry.Compare(reg, id, list).Drift; len(drift) > 0 {
		var fields []string
		for _, d := range drift {
			fields = append(fields, d.Model+"."+d.Field)
		}
		return fmt.Sprintf("%s reports different facts for %s (bud registry refresh <registry.jsonc> %s shows them)", id, strings.Join(fields, ", "), id)
	}

	return ""
}
