package mcpops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"jgarner.dev/bud/internal/doctor"
	"jgarner.dev/bud/internal/paths"
	"jgarner.dev/bud/internal/syncer"
)

// Sources is what the tools read, swappable in tests
type Sources struct {
	// Doctor runs bud doctor's checks
	Doctor func(doctor.Options) []doctor.Result
	// Paths resolves bud's paths, for the ledger and the sync lock
	Paths func() (*paths.Paths, error)
}

// Defaults reads this machine
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
		notBuilt("spend", "Router spend against the C2 cap (about $10 a month of Go top-up).",
			"router spend isn't built yet: the model gateway that counts it comes in build plan phase 3 (Suite D70)"),
		notBuilt("approvals", "The approval queue: actions waiting on Jason's yes or no.",
			"the approval queue isn't built yet: it comes with the approval service (build plan phase 7) and the shared approval log (JGA-348)"),
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
