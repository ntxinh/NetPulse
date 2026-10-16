// Package tailscale polls `tailscale status --json` and commits parsed
// snapshots to the store. A missing binary or stopped daemon are distinct
// menu states, not failures: binary missing → Backend "unavailable";
// daemon stopped → Backend "Stopped". Parse errors keep the last-good
// snapshot.
package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/exodia/netpulse/internal/state"
)

type peerJSON struct {
	HostName     string
	DNSName      string
	TailscaleIPs []string
	Online       bool
	ExitNode     bool // is an exit node option, not the one in use
}

type statusJSON struct {
	BackendState   string
	Self           peerJSON
	ExitNodeStatus *peerJSON
	Peer           map[string]peerJSON
}

// Parse maps `tailscale status --json` output to a TailscaleSnap.
// Peers are sorted by name for a stable menu.
func Parse(data []byte) (state.TailscaleSnap, error) {
	var st statusJSON
	if err := json.Unmarshal(data, &st); err != nil {
		return state.TailscaleSnap{}, err
	}
	snap := state.TailscaleSnap{
		Backend: st.BackendState,
		SelfIP:  firstOr(st.Self.TailscaleIPs, ""),
	}
	if ex := st.ExitNodeStatus; ex != nil && ex.Online {
		snap.ExitNode = firstOr(ex.TailscaleIPs, trimDot(ex.DNSName))
	}
	for _, p := range st.Peer {
		name := p.HostName
		if name == "" {
			name = trimDot(p.DNSName)
		}
		snap.Peers = append(snap.Peers, state.Peer{
			Name:   name,
			IP:     firstOr(p.TailscaleIPs, ""),
			Online: p.Online,
			Exit:   p.ExitNode,
		})
	}
	sort.Slice(snap.Peers, func(i, j int) bool { return snap.Peers[i].Name < snap.Peers[j].Name })
	return snap, nil
}

func firstOr(ips []string, fallback string) string {
	if len(ips) > 0 {
		return ips[0]
	}
	return fallback
}

func trimDot(s string) string { return strings.TrimSuffix(s, ".") }

// Poll runs `tailscale status --json` every interval and commits.
// execFn injectable for tests; nil → real exec.
func Poll(ctx context.Context, interval time.Duration, store *state.Store, execFn func(ctx context.Context) ([]byte, error)) {
	if execFn == nil {
		execFn = func(ctx context.Context) ([]byte, error) {
			return exec.CommandContext(ctx, "tailscale", "status", "--json").Output()
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	unavailable := false

	poll := func() {
		out, err := execFn(ctx)
		switch {
		case errors.Is(err, exec.ErrNotFound):
			if !unavailable { // commit once, then poll at 4x to detect install
				store.CommitTailscale(state.TailscaleSnap{Backend: "unavailable"})
				ticker.Reset(4 * interval)
				unavailable = true
			}
		case err != nil:
			log.Printf("tailscale: exec: %v", err)
		default:
			snap, err := Parse(out)
			if err != nil {
				log.Printf("tailscale: parse: %v (keeping last snapshot)", err)
				return
			}
			store.CommitTailscale(snap)
			if unavailable {
				ticker.Reset(interval)
				unavailable = false
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		poll()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
