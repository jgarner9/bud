package mcpops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"jgarner.dev/bud/internal/approval"
	"jgarner.dev/bud/internal/doctor"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/registry"
	"jgarner.dev/bud/internal/router"
	"jgarner.dev/bud/internal/syncer"
)

// Sources is what the tools read, swappable in tests
type Sources struct {
	// Doctor runs bud doctor's checks
	Doctor func(doctor.Options) []doctor.Result
	// Paths resolves bud's paths, for the ledger and the sync lock
	Paths func() (*paths.Paths, error)
	// Registry loads the model registry, for each model's cost class and
	// allowance; nil, or an error, leaves those out of spend
	Registry func() (*registry.Registry, error)
}

// Defaults reads this machine, with no registry; bud mcp-ops adds the
// synced one
func Defaults() Sources {
	return Sources{Doctor: doctor.Run, Paths: paths.Resolve}
}

// Tools are the ops server's tools, in the order tools/list gives them
func Tools(src Sources) []Tool {
	return []Tool{
		{
			Name:        "doctor",
			Description: "Runs bud doctor on the home server and returns each check's status (ok, warn, fail), what it found and the fix. Use it to answer whether everything is healthy. It runs offline: the network checks (bud doctor --net) are left out, since P0 sessions may call this tool.",
			Run: func(_ context.Context, args json.RawMessage) (any, error) {
				if err := decodeArgs(args, &struct{}{}); err != nil {
					return nil, err
				}
				// never Net: the server can't see the session's tier (the
				// gateway strips it), and P0 sessions may call this, so
				// nothing here reaches outside the house (D81)
				return doctorReport(src.Doctor(doctor.Options{Quick: true})), nil
			},
		},
		{
			Name:        "sync_status",
			Description: "Returns what bud sync left on the home server: the device profile, the channel, the content version in use, the overlay repo and commit, versions rolled back from, and drift (files bud wrote that were changed by hand or are missing).",
			Run: func(_ context.Context, args json.RawMessage) (any, error) {
				if err := decodeArgs(args, &struct{}{}); err != nil {
					return nil, err
				}
				return syncStatus(src.Paths)
			},
		},
		{
			Name:        "spend",
			Description: "Returns this month's model spend as bud router counted it: the top-up total against the router's monthly cap (about $10 of Go top-up), and each model's dollars spent, with its cost class and, for an included model, its allowance and what's left. Read-only; months are UTC.",
			Run: func(_ context.Context, args json.RawMessage) (any, error) {
				if err := decodeArgs(args, &struct{}{}); err != nil {
					return nil, err
				}
				return routerSpend(src, time.Now())
			},
		},
		{
			Name:        "approvals",
			Description: "Counts the approval requests waiting on Jason's yes or no, in total and by action (such as mcp:linear/save_issue). Ids, targets and arguments are left out, so nothing here can approve or replay a request. Read-only.",
			Run: func(_ context.Context, args json.RawMessage) (any, error) {
				if err := decodeArgs(args, &struct{}{}); err != nil {
					return nil, err
				}
				return pendingApprovals(src.Paths, time.Now())
			},
		},
		notBuilt("reviews", "The review queue: staged skill and memory writes waiting on Jason's review.",
			"the review queue isn't built yet: staged skill and memory writes come with Hermes (Suite D48) and the memory curator (build plan phase 5)"),
	}
}

func notBuilt(name, what, why string) Tool {
	return Tool{
		Name:        name,
		Description: what + " Not built yet: it reports that, rather than an empty queue.",
		Run: func(context.Context, json.RawMessage) (any, error) {
			return nil, errors.New(why)
		},
	}
}

// decodeArgs reads a tool's arguments into v, refusing names it doesn't
// take. No arguments, or null, leave v as it is
func decodeArgs(args json.RawMessage, v any) error {
	args = bytes.TrimSpace(args)
	if len(args) == 0 || string(args) == "null" {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(args))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return &ArgError{Msg: "bad arguments: " + strings.TrimPrefix(err.Error(), "json: ")}
	}
	return nil
}

type modelSpend struct {
	ID    string  `json:"id"`
	Cost  string  `json:"cost,omitempty"`
	Spent float64 `json:"spent"`
	// Allowance and AllowanceLeft are set for an included (C1) model with
	// an allowance
	Allowance     *float64 `json:"allowance,omitempty"`
	AllowanceLeft *float64 `json:"allowance_left,omitempty"`
}

type spendOut struct {
	Month string   `json:"month"`
	Note  string   `json:"note,omitempty"`
	TopUp float64  `json:"topup"`
	Cap   *float64 `json:"topup_cap,omitempty"`
	Left  *float64 `json:"topup_left,omitempty"`
	// Models lists the models with spend this month, by id
	Models []modelSpend `json:"models"`
}

// cents rounds dollars to the cent, so float sums read cleanly
func cents(v float64) float64 { return math.Round(v*100) / 100 }

// routerSpend reads the router's spend ledger without writing to it
func routerSpend(src Sources, now time.Time) (spendOut, error) {
	p, err := src.Paths()
	if err != nil {
		return spendOut{}, err
	}
	sp, err := router.ReadSpend(p.RouterSpend(), now)
	if err != nil {
		return spendOut{}, err
	}
	out := spendOut{Month: sp.Month, TopUp: cents(sp.TopUp), Models: []modelSpend{}}
	var notes []string
	if !sp.Recorded {
		notes = append(notes, "the router hasn't charged anything on this server yet")
	}
	if sp.Cap != nil {
		c, left := cents(*sp.Cap), cents(math.Max(0, *sp.Cap-sp.TopUp))
		out.Cap, out.Left = &c, &left
	} else if sp.Recorded {
		notes = append(notes, "the cap isn't in the ledger yet; the router records it with its next charge")
	}
	var reg *registry.Registry
	if src.Registry != nil {
		if reg, err = src.Registry(); err != nil {
			reg = nil
			notes = append(notes, "cost classes and allowances are left out: "+err.Error())
		}
	}
	ids := make([]string, 0, len(sp.Spent))
	for id := range sp.Spent {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := modelSpend{ID: id, Spent: cents(sp.Spent[id])}
		if reg != nil {
			if rm, ok := reg.Models[id]; ok {
				m.Cost = rm.Cost
				if rm.Cost == "C1" && rm.Allowance > 0 {
					a, left := cents(rm.Allowance), cents(math.Max(0, rm.Allowance-sp.Spent[id]))
					m.Allowance, m.AllowanceLeft = &a, &left
				}
			}
		}
		out.Models = append(out.Models, m)
	}
	out.Note = strings.Join(notes, "; ")
	return out, nil
}

type approvalsOut struct {
	Pending  int            `json:"pending"`
	ByAction map[string]int `json:"by_action"`
}

// pendingApprovals reads the approval store without writing to it
func pendingApprovals(resolve func() (*paths.Paths, error), now time.Time) (approvalsOut, error) {
	p, err := resolve()
	if err != nil {
		return approvalsOut{}, err
	}
	counts, err := approval.Store{Path: p.ApprovalStore()}.PendingCounts(now)
	if err != nil {
		return approvalsOut{}, fmt.Errorf("read the approval store: %w", err)
	}
	out := approvalsOut{ByAction: counts}
	for _, n := range counts {
		out.Pending += n
	}

	return out, nil
}

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

type doctorOut struct {
	Failed   int     `json:"failed"`
	Warnings int     `json:"warnings"`
	Results  []check `json:"results"`
}

// doctorReport is bud doctor --json's shape, with the counts first
func doctorReport(results []doctor.Result) doctorOut {
	out := doctorOut{Results: make([]check, 0, len(results))}
	for _, r := range results {
		switch r.Status {
		case doctor.Fail:
			out.Failed++
		case doctor.Warn:
			out.Warnings++
		}
		out.Results = append(out.Results, check{r.Name, strings.ToLower(r.Status.String()), r.Detail, r.Fix})
	}
	return out
}

type drift struct {
	Path  string `json:"path"`
	State string `json:"state"` // changed or missing
}

type syncOut struct {
	Synced     bool               `json:"synced"`
	Note       string             `json:"note,omitempty"`
	Device     string             `json:"device,omitempty"`
	Channel    string             `json:"channel,omitempty"`
	Content    string             `json:"content,omitempty"`
	Current    string             `json:"current,omitempty"`
	Overlay    *syncer.OverlayRef `json:"overlay,omitempty"`
	RolledBack []string           `json:"rolled_back,omitempty"`
	Drift      []drift            `json:"drift"`
}

// syncStatus reads the ledger and checks its files, holding the sync lock
// so the ledger and the links agree
func syncStatus(resolve func() (*paths.Paths, error)) (syncOut, error) {
	p, err := resolve()
	if err != nil {
		return syncOut{}, err
	}
	unlock, err := syncer.TryLock(p)
	if errors.Is(err, syncer.ErrBusy) {
		return syncOut{}, errors.New("a bud sync or bud doctor is using the sync ledger right now; ask again in a moment")
	}
	if err != nil {
		return syncOut{}, fmt.Errorf("take the sync lock: %w", err)
	}
	defer unlock()

	led, err := syncer.LoadLedger(p.SyncLedger())
	if err != nil {
		return syncOut{}, err
	}
	out := syncOut{Drift: []drift{}}
	if led.Content == "" {
		out.Note = "bud sync hasn't run on this device"
		return out, nil
	}
	out.Synced = true
	out.Device, out.Channel, out.Content = led.Device, led.Channel, led.Content
	out.Current = syncer.CurrentLink(p)
	out.Overlay, out.RolledBack = led.Overlay, led.RolledBack
	ds, err := led.Drift()
	if err != nil {
		return syncOut{}, fmt.Errorf("check the synced files: %w", err)
	}
	for _, d := range ds {
		state := "changed"
		if d.Missing {
			state = "missing"
		}
		out.Drift = append(out.Drift, drift{d.Path, state})
	}
	return out, nil
}
