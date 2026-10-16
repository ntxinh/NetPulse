// Package routes monitors watched prefixes in the kernel routing table
// via `ip -j route show` and commits the missing set to the store.
// "default" is a virtual prefix: ip outputs dst:"default" literally, so
// no special-casing is needed beyond the watch list itself.
//
// ponytail: no netlink lib; if `ip -j` output proves fragile, revisit.
package routes

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os/exec"
	"time"

	"github.com/exodia/netpulse/internal/state"
)

// Missing returns watched prefixes absent from the `ip -j route` table.
// A nil/unparsable table means everything is missing.
func Missing(table []byte, watch []string) []string {
	var rows []struct {
		Dst string `json:"dst"`
	}
	_ = json.Unmarshal(table, &rows)
	present := make(map[string]bool, len(rows))
	for _, r := range rows {
		present[r.Dst] = true
	}
	var missing []string
	for _, w := range watch {
		if !present[w] {
			missing = append(missing, w)
		}
	}
	return missing
}

// Poll runs `ip -j route show` every interval and commits the missing
// set. Exec errors commit nothing; if ip itself is absent (it ships with
// iproute2 on every Fedora install) the error is logged once.
func Poll(ctx context.Context, interval time.Duration, watch []string, store *state.Store) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	missingLogged := false

	poll := func() {
		out, err := exec.CommandContext(ctx, "ip", "-j", "route", "show").Output()
		if err != nil {
			if !errors.Is(err, exec.ErrNotFound) || !missingLogged {
				log.Printf("routes: exec: %v", err)
				missingLogged = errors.Is(err, exec.ErrNotFound)
			}
			return
		}
		store.CommitRoutes(Missing(out, watch))
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
