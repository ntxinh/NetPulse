// Package health runs per-check probers on individual schedules and feeds
// transition-only results into the state store.
package health

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/exodia/netpulse/internal/config"
	"github.com/exodia/netpulse/internal/state"
)

// Prober runs one probe attempt. latency is meaningful only when err == nil.
type Prober func(ctx context.Context, timeout time.Duration) (latency time.Duration, err error)

// ForCheck builds a Prober for one check. dns checks use dnsTargets/dnsServers
// (config [dns] section); unknown Type returns an error.
func ForCheck(c config.Check, dnsTargets, dnsServers []string) (Prober, error) {
	switch c.Type {
	case "icmp":
		return icmpProber(c.Target), nil
	case "tcp":
		return tcpProber(c.Target), nil
	case "http":
		return httpProber(c.Target), nil
	case "dns":
		return dnsProber(dnsTargets, dnsServers), nil
	default:
		return nil, fmt.Errorf("unknown check type %q", c.Type)
	}
}

// counter tracks consecutive failures for one check and reports only
// transitions: first observation, threshold crossing, recovery.
type counter struct {
	limit int
	n     int
	down  bool
	up    bool // an ok result has been reported at least once
}

// observe returns (Result, changed). changed=false means below threshold /
// steady state and nothing should be committed.
func (c *counter) observe(name string, crit bool, lat time.Duration, err error) (state.Result, bool) {
	if err == nil {
		c.n = 0
		c.down = false
		if !c.up {
			c.up = true
			return state.Result{Name: name, Ok: true, Critical: crit, Latency: lat}, true
		}
		return state.Result{}, false
	}
	c.n++
	if c.n >= c.limit && !c.down {
		c.down = true
		c.up = false
		return state.Result{Name: name, Ok: false, Critical: crit, Err: err.Error()}, true
	}
	return state.Result{}, false
}

// acc accumulates the latest reported result per check and commits a sorted
// snapshot to the store.
// ponytail: shared map under mutex, committed on each goroutine's tick —
// O(#checks) copy per tick, fine at tens of checks.
type acc struct {
	mu      sync.Mutex
	results map[string]state.Result
	store   *state.Store
}

func (a *acc) commit() {
	a.mu.Lock()
	rs := make([]state.Result, 0, len(a.results))
	for _, r := range a.results {
		rs = append(rs, r)
	}
	a.mu.Unlock()
	sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
	a.store.CommitHealth(state.HealthSnap{Results: rs})
}

// Run launches one goroutine per check; results feed the store via
// accumulate-commit. Cancel ctx to stop all check goroutines (config reload
// calls this). Checks whose prober fails to build get a single down result
// and no goroutine — worker-internal errors never enter the failure counter.
func Run(ctx context.Context, checks []config.Check, dnsTargets, dnsServers []string, store *state.Store) {
	a := &acc{results: make(map[string]state.Result, len(checks)), store: store}
	for _, chk := range checks {
		p, err := ForCheck(chk, dnsTargets, dnsServers)
		if err != nil {
			a.results[chk.Name] = state.Result{
				Name:     chk.Name,
				Critical: chk.IsCritical(),
				Err:      "prober: " + err.Error(),
			}
			continue
		}
		go worker(ctx, chk, p, a)
	}
	if len(a.results) > 0 {
		a.commit()
	}
}

func worker(ctx context.Context, chk config.Check, p Prober, a *acc) {
	c := &counter{limit: chk.Failures}
	timeout := time.Duration(chk.TimeoutSecs) * time.Second
	tick := func() {
		pctx, cancel := context.WithTimeout(ctx, timeout)
		lat, err := p(pctx, timeout)
		cancel()
		if r, ok := c.observe(chk.Name, chk.IsCritical(), lat, err); ok {
			a.mu.Lock()
			a.results[chk.Name] = r
			a.mu.Unlock()
			a.commit()
		}
	}
	tick()
	t := time.NewTicker(time.Duration(chk.IntervalSecs) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
