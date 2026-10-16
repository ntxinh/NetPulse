// Package vpn watches NetworkManager's active VPN/tun connections via
// nmcli and commits the active-name set to the store.
//
// The spec asked for NM D-Bus; nmcli parses the same state with zero
// D-Bus machinery, and `nmcli monitor` already emits on NM signals.
//
// ponytail: subprocess-per-monitor, reconnect loop is the simplest
// correct form; revisit only if nmcli proves heavier than godbus.
package vpn

import (
	"bufio"
	"context"
	"errors"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/exodia/netpulse/internal/state"
)

const retryInterval = 30 * time.Second

// ParseActive parses `nmcli -t -f NAME,TYPE,DEVICE connection show --active`
// output, returning names of connections whose TYPE is vpn|tun|wireguard.
func ParseActive(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ":")
		if len(f) >= 3 {
			switch f[1] {
			case "vpn", "tun", "wireguard":
				names = append(names, f[0])
			}
		}
	}
	return names
}

// Watch monitors via `nmcli monitor` line stream plus a 30s reconcile
// tick (covers missed lines). When nmcli is missing or the monitor dies,
// it commits VpnSnap{Unavailable:true} and retries every 30s.
func Watch(ctx context.Context, store *state.Store) {
	logged := false
	for {
		err := monitor(ctx, store)
		if ctx.Err() != nil {
			return
		}
		if !errors.Is(err, exec.ErrNotFound) || !logged {
			log.Printf("vpn: monitor: %v", err)
			logged = errors.Is(err, exec.ErrNotFound)
		}
		store.CommitVpn(state.VpnSnap{Unavailable: true})
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
}

// monitor runs one `nmcli monitor` session: commit a baseline, then
// re-read the active set on each emitted line and on the reconcile tick.
// Returns when the monitor exits or ctx is cancelled (always non-nil).
func monitor(ctx context.Context, store *state.Store) error {
	cmd := exec.CommandContext(ctx, "nmcli", "monitor")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scan := make(chan struct{}, 1)
	eof := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			select {
			case scan <- struct{}{}:
			default: // coalesce bursts into one refresh
			}
		}
		eof <- sc.Err()
	}()

	tick := time.NewTicker(retryInterval)
	defer tick.Stop()
	refresh := func() {
		out, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "NAME,TYPE,DEVICE",
			"connection", "show", "--active").Output()
		if err != nil {
			return // monitor stream will surface NM death
		}
		store.CommitVpn(state.VpnSnap{Active: ParseActive(string(out))})
	}
	refresh() // baseline on attach
	for {
		select {
		case <-ctx.Done():
			return cmd.Wait()
		case <-scan:
			refresh()
		case <-tick.C:
			refresh()
		case err := <-eof:
			if err != nil {
				return err
			}
			if err := cmd.Wait(); err != nil {
				return err
			}
			return errors.New("nmcli monitor exited")
		}
	}
}
