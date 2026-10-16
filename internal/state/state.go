// Package state holds NetPulse's shared snapshot store. Workers commit
// snapshots; the store diffs prev vs new, emits transition events, and
// fans them out to subscribers (tray, alert adapters).
package state

import (
	"sync"
	"time"
)

type Severity int

const (
	SevOk       Severity = iota // green
	SevDegraded                 // amber
	SevDown                     // red
)

type Iface struct {
	Name  string
	Up    bool
	MTU   int
	IPv4  string // "" if none
	RxBps uint64 // bytes/sec since last snapshot
	TxBps uint64
}

type SysSnap struct {
	Ifaces []Iface
	Err    error
}

type VpnSnap struct {
	Active      []string // active vpn/tun connection names
	Unavailable bool     // NetworkManager unreachable
}

type Peer struct {
	Name, IP     string
	Online, Exit bool
}

type TailscaleSnap struct {
	Backend  string // "Running", "Stopped", "NeedsLogin", "unavailable"
	SelfIP   string
	ExitNode string // "" if none
	Peers    []Peer
}

type Result struct {
	Name     string
	Ok       bool
	Critical bool
	Latency  time.Duration
	Err      string
}

type HealthSnap struct{ Results []Result }

type EventKind int

const (
	EvNone EventKind = iota
	EvRouteMissing
	EvRouteOk
	EvVpnChanged
	EvTailscaleChanged
	EvCheckDown
	EvCheckUp
	EvConfigReloaded
	EvSourceUnavailable
)

type Event struct {
	Kind    EventKind
	Subject string // check name, connection id, prefix
	Detail  string
}

type Store struct {
	mu     sync.Mutex
	sys    SysSnap
	vpn    VpnSnap
	ts     TailscaleSnap
	health HealthSnap
	subs   []chan Event

	// routesPrev holds all currently-missing watched prefixes; Severity is
	// red while any route is missing (spec §5: watched or default route).
	routesPrev map[string]bool
	// vpnDropped is set when all VPN connections drop and cleared when any
	// connection is active again. SevDown per spec §5 ("VPN dropped").
	vpnDropped bool
}

func NewStore() *Store {
	return &Store{routesPrev: make(map[string]bool)}
}

func (s *Store) CommitSys(n SysSnap) []Event {
	s.mu.Lock()
	var evs []Event
	if s.sys.Err == nil && n.Err != nil {
		evs = append(evs, Event{Kind: EvSourceUnavailable, Subject: "sysnet", Detail: n.Err.Error()})
	}
	s.sys = n
	s.mu.Unlock()
	s.fanout(evs)
	return evs
}

func (s *Store) CommitVpn(n VpnSnap) []Event {
	s.mu.Lock()
	var evs []Event
	prev := set(s.vpn.Active)
	next := set(n.Active)
	for _, name := range s.vpn.Active {
		if !next[name] {
			evs = append(evs, Event{Kind: EvVpnChanged, Subject: name, Detail: "dropped"})
		}
	}
	for _, name := range n.Active {
		if !prev[name] {
			evs = append(evs, Event{Kind: EvVpnChanged, Subject: name, Detail: "up"})
		}
	}
	if !s.vpn.Unavailable && n.Unavailable {
		evs = append(evs, Event{Kind: EvSourceUnavailable, Subject: "networkmanager"})
	}
	if len(s.vpn.Active) > 0 && len(n.Active) == 0 {
		s.vpnDropped = true
	} else if len(n.Active) > 0 {
		s.vpnDropped = false
	}
	s.vpn = n
	s.mu.Unlock()
	s.fanout(evs)
	return evs
}

func (s *Store) CommitTailscale(n TailscaleSnap) []Event {
	s.mu.Lock()
	var evs []Event
	if s.ts.Backend != n.Backend {
		evs = append(evs, Event{Kind: EvTailscaleChanged, Detail: s.ts.Backend + "→" + n.Backend})
	}
	s.ts = n
	s.mu.Unlock()
	s.fanout(evs)
	return evs
}

func (s *Store) CommitHealth(n HealthSnap) []Event {
	s.mu.Lock()
	prev := make(map[string]Result, len(s.health.Results))
	for _, r := range s.health.Results {
		prev[r.Name] = r
	}
	var evs []Event
	for _, r := range n.Results {
		// Prev missing → baseline is "was ok": a check first seen down
		// fires EvCheckDown (ok→!ok transition).
		prevOk := true
		if p, seen := prev[r.Name]; seen {
			prevOk = p.Ok
		}
		switch {
		case prevOk && !r.Ok:
			evs = append(evs, Event{Kind: EvCheckDown, Subject: r.Name, Detail: r.Err})
		case !prevOk && r.Ok:
			evs = append(evs, Event{Kind: EvCheckUp, Subject: r.Name})
		}
	}
	s.health = n
	s.mu.Unlock()
	s.fanout(evs)
	return evs
}

// CommitRoutes: missing = watched prefixes absent from the routing table.
// Severity is red while any watched prefix is missing.
// First commit establishes the baseline silently.
func (s *Store) CommitRoutes(missing []string) []Event {
	s.mu.Lock()
	var evs []Event
	next := set(missing)
	for _, prefix := range missing {
		if !s.routesPrev[prefix] {
			evs = append(evs, Event{Kind: EvRouteMissing, Subject: prefix})
		}
	}
	for prefix := range s.routesPrev {
		if !next[prefix] {
			evs = append(evs, Event{Kind: EvRouteOk, Subject: prefix})
		}
	}
	s.routesPrev = next
	s.mu.Unlock()
	s.fanout(evs)
	return evs
}

// EmitConfigReloaded notifies subscribers that config was reloaded.
func (s *Store) EmitConfigReloaded() { s.Emit(Event{Kind: EvConfigReloaded}) }

// Emit fans out an ad-hoc event that doesn't come from a snapshot diff
// (e.g. invalid TOML on reload).
func (s *Store) Emit(e Event) { s.fanout([]Event{e}) }

// Subscribe returns a buffered channel (cap 64) receiving every emitted event.
// Delivery is non-blocking: a full subscriber drops events rather than
// stalling commits.
func (s *Store) Subscribe() chan Event {
	ch := make(chan Event, 64)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

func (s *Store) Snapshot() (SysSnap, VpnSnap, TailscaleSnap, HealthSnap) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sys, s.vpn, s.ts, s.health
}

// Severity:
//
//	SevDown: any critical check down, ts.Backend in {"Stopped","NeedsLogin"},
//	         any watched route missing (tracked via CommitRoutes),
//	         or all VPN connections dropped after having been active.
//	SevDegraded: non-critical check down, any Unavailable source, or
//	             ts.Backend == "unavailable".
//	else SevOk.
func (s *Store) Severity() Severity {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.health.Results {
		if !r.Ok {
			if r.Critical {
				return SevDown
			}
		}
	}
	if s.ts.Backend == "Stopped" || s.ts.Backend == "NeedsLogin" {
		return SevDown
	}
	if len(s.routesPrev) > 0 || s.vpnDropped {
		return SevDown
	}
	for _, r := range s.health.Results {
		if !r.Ok {
			return SevDegraded
		}
	}
	if s.vpn.Unavailable || s.sys.Err != nil || s.ts.Backend == "unavailable" {
		return SevDegraded
	}
	return SevOk
}

func set(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func (s *Store) fanout(evs []Event) {
	if len(evs) == 0 {
		return
	}
	s.mu.Lock()
	subs := append([]chan Event(nil), s.subs...)
	s.mu.Unlock()
	for _, ch := range subs {
		for _, ev := range evs {
			select {
			case ch <- ev:
			default: // slow subscriber drops, never block commits
			}
		}
	}
}
