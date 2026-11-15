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

	"jgarner.dev/bud/internal/config"
	"jgarner.dev/bud/internal/content"
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

func checkSync(p *paths.Paths, cfg config.Settings, net bool) []Result {
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
	led, ledResult := checkLedger(p)
	results = append(results, ledResult)
	if led == nil || led.Content == "" || ledResult.Status == Fail {
		return results
	}

	reg, regResult := checkRegistry(filepath.Join(p.Content(), led.Content), overlayDir)
	results = append(results, regResult)
	if prof != nil && reg != nil {
		results = append(results, checkRender(p, cfg, repo, led))
	}
	if !net {
		return results
	}

	ctx, cancel := context.WithTimeout(context.Background(), netTimeout)
	defer cancel()
	if prof != nil {
		results = append(results, checkChannel(ctx, cfg.Content.Mirror, prof, led))
	}
	if reg != nil {
		results = append(results, checkEndpoints(ctx, reg))
	}

	return results
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
