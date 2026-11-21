package router

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultProbeInterval = 10 * time.Second
	defaultProbeTimeout  = 2 * time.Second
	// recoverAfter is how long a provider that isn't probed stays down
	// after a failed request, before the router tries it again
	recoverAfter = 30 * time.Second
)

// health keeps each provider's up or down state (Suite D104)
type health struct {
	mu     sync.Mutex
	now    func() time.Time
	logf   func(string, ...any)
	state  map[string]*provState
	probed map[string]bool // providers whose way back up is a probe
}

type provState struct {
	down  bool
	since time.Time
}

func newHealth(now func() time.Time, logf func(string, ...any), probed map[string]bool) *health {
	return &health{now: now, logf: logf, state: map[string]*provState{}, probed: probed}
}

func (h *health) set(provider string, down bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.state[provider]
	if st == nil {
		st = &provState{}
		h.state[provider] = st
	}
	if st.down == down {
		return
	}
	st.down, st.since = down, h.now()
	if down {
		h.logf("router: provider %s is down", provider)
	} else {
		h.logf("router: provider %s is up again", provider)
	}
}

func (h *health) markDown(provider string) { h.set(provider, true) }
func (h *health) markUp(provider string)   { h.set(provider, false) }

// isDown reports whether requests to the provider should be avoided. A
// provider that isn't probed counts as up again recoverAfter after it was
// marked down
func (h *health) isDown(provider string) bool {
	h.mu.Lock()
	st := h.state[provider]
	if st == nil || !st.down {
		h.mu.Unlock()
		return false
	}
	if h.probed[provider] || h.now().Sub(st.since) < recoverAfter {
		h.mu.Unlock()
		return true
	}
	h.mu.Unlock()
	h.markUp(provider)
	return false
}

// Start probes the local providers once, before returning so that state is
// known before serving, then every probe interval until ctx ends
func (rt *Router) Start(ctx context.Context) {
	rt.probeAll(ctx)
	if len(rt.health.probed) == 0 {
		return
	}
	go func() {
		t := time.NewTicker(rt.probeEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				rt.probeAll(ctx)
			}
		}
	}()
}

func (rt *Router) probeAll(ctx context.Context) {
	var wg sync.WaitGroup
	for pid := range rt.health.probed {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rt.probe(ctx, pid) {
				rt.health.markUp(pid)
			} else if ctx.Err() == nil {
				rt.health.markDown(pid)
			}
		}()
	}
	wg.Wait()
}

// probe asks the provider for its model list; any 2xx means up
func (rt *Router) probe(ctx context.Context, pid string) bool {
	ctx, cancel := context.WithTimeout(ctx, rt.probeTimeout)
	defer cancel()
	u := *rt.provURL[pid]
	u.Path = strings.TrimSuffix(u.Path, "/") + "/models"
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false
	}
	if k := rt.provKey[pid]; k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
