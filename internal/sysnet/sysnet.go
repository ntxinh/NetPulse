// Package sysnet scans /sys/class/net for interface state and computes
// per-interface throughput from byte-counter deltas across polls.
package sysnet

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/exodia/netpulse/internal/state"
)

// Scan reads sysfsRoot/class/net, merges the addr strings from addrs
// (see HostAddrs), and computes Bps from counter deltas vs prev over dt.
// prev is the map returned by the previous Scan; nil on first call.
// The returned map replaces prev for the next call.
func Scan(sysfsRoot string, addrs map[string]string, prev map[string][2]uint64, dt time.Duration) (state.SysSnap, map[string][2]uint64) {
	base := filepath.Join(sysfsRoot, "class", "net")
	ents, err := os.ReadDir(base)
	if err != nil {
		return state.SysSnap{Err: err}, prev
	}
	ifaces := make([]state.Iface, 0, len(ents))
	next := make(map[string][2]uint64, len(ents))
	secs := dt.Seconds()
	for _, e := range ents {
		// class/net entries are symlinks; don't gate on IsDir.
		dir := filepath.Join(base, e.Name())
		rx := readUint(dir, "statistics", "rx_bytes")
		tx := readUint(dir, "statistics", "tx_bytes")
		next[e.Name()] = [2]uint64{rx, tx}
		iface := state.Iface{
			Name: e.Name(),
			Up:   strings.TrimSpace(readStr(dir, "operstate")) == "up",
			MTU:  int(readUint(dir, "mtu")),
			// IPv4 holds the best display addr (v4 preferred); "" if none.
			IPv4: addrs[e.Name()],
		}
		if p, ok := prev[e.Name()]; ok && secs > 0 {
			// counter wrap/reset leaves a0 underflow → 0, not a huge rate
			if rx >= p[0] {
				iface.RxBps = uint64(float64(rx-p[0]) / secs)
			}
			if tx >= p[1] {
				iface.TxBps = uint64(float64(tx-p[1]) / secs)
			}
		}
		ifaces = append(ifaces, iface)
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })
	return state.SysSnap{Ifaces: ifaces}, next
}

// HostAddrs maps each interface name to its first non-loopback address,
// IPv4 preferred; interfaces with only loopback addrs are omitted.
func HostAddrs() (map[string]string, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(ifs))
	for _, ifi := range ifs {
		as, err := ifi.Addrs()
		if err != nil {
			continue
		}
		first := ""
		for _, a := range as {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || ip.IsLoopback() {
				continue
			}
			s := ip.String()
			if first == "" {
				first = s
			}
			if ip4 := ip.To4(); ip4 != nil {
				first = ip4.String()
				break // v4 wins
			}
		}
		if first != "" {
			out[ifi.Name] = first
		}
	}
	return out, nil
}

// Poll scans sysfsRoot every interval and commits SysSnaps to the store.
// The counter map is kept in the closure so Bps spans ticks.
func Poll(ctx context.Context, sysfsRoot string, interval time.Duration, store *state.Store) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var prev map[string][2]uint64
	last := time.Now()

	tick := func() {
		now := time.Now()
		dt := now.Sub(last)
		addrs, err := HostAddrs()
		if err != nil {
			store.CommitSys(state.SysSnap{Err: err})
			return
		}
		snap, next := Scan(sysfsRoot, addrs, prev, dt)
		if snap.Err == nil {
			prev, last = next, now
		}
		store.CommitSys(snap)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		tick()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func readStr(elems ...string) string {
	b, err := os.ReadFile(filepath.Join(elems...))
	if err != nil {
		return ""
	}
	return string(b)
}

func readUint(elems ...string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(readStr(elems...)), 10, 64)
	return n
}
