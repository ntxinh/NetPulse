// Package tray adapts the state store to a StatusNotifierItem tray icon
// and dbusmenu: a severity-colored dot plus a live menu rebuilt on each
// state event (250ms debounce).
package tray

import (
	"context"
	"fmt"
	"time"

	"fyne.io/systray"
	"github.com/exodia/netpulse/internal/state"
	"github.com/godbus/dbus/v5"
)

// Run starts the systray adapter and blocks until ctx is done (which calls
// systray.Quit). It returns an error immediately when no
// org.kde.StatusNotifierWatcher is present on the session bus — the caller
// falls back to notifications-only.
func Run(ctx context.Context, store *state.Store, reload func(), quit func()) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("tray: session bus: %w", err)
	}
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		conn.Close()
		return fmt.Errorf("tray: list names: %w", err)
	}
	conn.Close()
	found := false
	for _, n := range names {
		if n == "org.kde.StatusNotifierWatcher" {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("tray: no StatusNotifierWatcher on session bus")
	}

	go func() {
		<-ctx.Done()
		systray.Quit()
	}()

	systray.Run(
		func() { onReady(ctx, store, reload, quit) },
		func() {},
	)
	// systray.Run returns only after systray.Quit() — normally ctx-driven.
	// An early return (e.g. internal loop exit) without ctx cancel means the
	// tray is dead: report it so the caller degrades to notify-only.
	if ctx.Err() == nil {
		return fmt.Errorf("tray: systray loop exited unexpectedly")
	}
	return nil
}

func onReady(ctx context.Context, store *state.Store, reload func(), quit func()) {
	systray.SetTitle("NetPulse")
	systray.SetTooltip("NetPulse")

	var dead chan struct{}
	rebuild := func() {
		if dead != nil {
			close(dead)
		}
		dead = make(chan struct{})
		systray.ResetMenu()
		systray.SetIcon(IconPNG(store.Severity()))
		buildMenu(store, reload, quit, dead)
	}

	evs := store.Subscribe()
	rebuild()

	go eventLoop(ctx, evs, rebuild)
}

// eventLoop rebuilds on each store event, coalesced by a 250ms debounce.
// Silent commits (sys throughput, Err→nil recovery) emit no events, so a
// 2s tick keeps the menu fresh — rebuild is cheap.
func eventLoop(ctx context.Context, evs chan state.Event, rebuild func()) {
	var timerC <-chan time.Time
	var timer *time.Timer
	refresh := time.NewTicker(2 * time.Second)
	defer refresh.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
			rebuild()
		case _, ok := <-evs:
			if !ok {
				return
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(250 * time.Millisecond)
			timerC = timer.C
		case <-timerC:
			timerC = nil
			rebuild()
		}
	}
}

func buildMenu(store *state.Store, reload func(), quit func(), dead chan struct{}) {
	sys, vpn, ts, health := store.Snapshot()

	title := systray.AddMenuItem("NetPulse", "")
	title.Disable()

	for _, iface := range sys.Ifaces {
		addr := iface.IPv4
		if addr == "" {
			addr = "no addr"
		}
		label := fmt.Sprintf("%s ▸ %s  ↓%s ↑%s", iface.Name, addr, fmtBps(iface.RxBps), fmtBps(iface.TxBps))
		if !iface.Up {
			label += " (down)"
		}
		item := systray.AddMenuItem(label, "")
		item.Disable()
	}

	systray.AddSeparator()

	if vpn.Unavailable {
		item := systray.AddMenuItem("VPN: unavailable", "")
		item.Disable()
	} else if len(vpn.Active) == 0 {
		item := systray.AddMenuItem("VPN: none", "")
		item.Disable()
	} else {
		for _, name := range vpn.Active {
			item := systray.AddMenuItem(fmt.Sprintf("VPN: %s — active", name), "")
			item.Disable()
		}
	}

	tsItem := systray.AddMenuItem(tsLabel(ts), "")
	for _, p := range ts.Peers {
		status := "offline"
		if p.Online {
			status = "online"
		}
		if p.Exit {
			status += ", exit node"
		}
		peer := tsItem.AddSubMenuItem(fmt.Sprintf("peer %q %s (%s)", p.Name, p.IP, status), "")
		peer.Disable()
	}

	systray.AddSeparator()

	checksItem := systray.AddMenuItem("Checks", "")
	if len(health.Results) == 0 {
		none := checksItem.AddSubMenuItem("none configured", "")
		none.Disable()
	}
	for _, r := range health.Results {
		label := fmt.Sprintf("%s — ok (%s)", r.Name, r.Latency.Round(time.Millisecond))
		if !r.Ok {
			label = r.Name + " — DOWN"
			if r.Err != "" {
				label += ": " + r.Err
			}
		}
		item := checksItem.AddSubMenuItem(label, "")
		item.Disable()
	}

	systray.AddSeparator()

	watchClick(systray.AddMenuItem("Reload config", ""), dead, reload)
	watchClick(systray.AddMenuItem("Quit", ""), dead, quit)
}

var tsBackendLabel = map[string]string{
	"Running":    "connected",
	"":           "unavailable",
	"Stopped":    "stopped",
	"NeedsLogin": "needs login",
}

func tsLabel(ts state.TailscaleSnap) string {
	label, ok := tsBackendLabel[ts.Backend]
	if !ok {
		label = ts.Backend
	}
	s := "Tailscale: " + label
	if ts.ExitNode != "" {
		s += " · exit: " + ts.ExitNode
	}
	return s
}

// watchClick invokes fn each time the item is clicked until the item is
// discarded by a menu rebuild (dead closed) or removed (ClickedCh closed).
func watchClick(item *systray.MenuItem, dead chan struct{}, fn func()) {
	go func() {
		for {
			select {
			case <-dead:
				return
			case _, ok := <-item.ClickedCh:
				if !ok {
					return
				}
				fn()
			}
		}
	}()
}

// fmtBps formats a bytes-per-second count: "0", "999B/s", "1.2KB/s", "1.2MB/s".
func fmtBps(b uint64) string {
	switch {
	case b == 0:
		return "0"
	case b < 1000:
		return fmt.Sprintf("%dB/s", b)
	case b < 1_000_000:
		return fmt.Sprintf("%.1fKB/s", float64(b)/1e3)
	default:
		return fmt.Sprintf("%.1fMB/s", float64(b)/1e6)
	}
}
