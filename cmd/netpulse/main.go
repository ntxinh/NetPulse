// Command netpulse is a system-tray network monitor: it polls interfaces,
// routes, VPN and Tailscale status, runs configurable health checks, and
// alerts on state transitions via desktop notifications.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/exodia/netpulse/internal/alert"
	"github.com/exodia/netpulse/internal/config"
	"github.com/exodia/netpulse/internal/health"
	"github.com/exodia/netpulse/internal/routes"
	"github.com/exodia/netpulse/internal/state"
	"github.com/exodia/netpulse/internal/sysnet"
	"github.com/exodia/netpulse/internal/tailscale"
	"github.com/exodia/netpulse/internal/tray"
	"github.com/exodia/netpulse/internal/vpn"
)

func main() {
	cfgPath := config.Path()
	if err := config.Ensure(cfgPath); err != nil {
		log.Printf("ensure config: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	store := state.NewStore()

	// alerter
	var as []alert.Alerter
	if conn, err := dbus.SessionBus(); err == nil {
		as = append(as, alert.NewNotify(conn))
	}
	if cfg.General.LogEvents {
		if l, err := alert.NewLog(stateLogPath()); err == nil {
			as = append(as, l)
		}
	}
	al := alert.Multi(as...)
	go func() {
		for e := range store.Subscribe() {
			al.Alert(e)
		}
	}()

	// workers — each recovered at the goroutine boundary; a panic marks the
	// source unavailable instead of taking down the process (spec §7).
	run := func(name string, f func()) {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					store.Emit(state.Event{Kind: state.EvSourceUnavailable, Subject: name, Detail: fmt.Sprint(r)})
				}
			}()
			f()
		}()
	}
	run("sysnet", func() {
		sysnet.Poll(ctx, "/sys", time.Duration(cfg.General.IfaceIntervalSecs)*time.Second, store)
	})
	run("routes", func() {
		routes.Poll(ctx, 5*time.Second, append([]string{"default"}, cfg.General.Routes...), store)
	})
	run("vpn", func() { vpn.Watch(ctx, store) })
	run("tailscale", func() {
		tailscale.Poll(ctx, time.Duration(cfg.General.TailscaleIntervalSecs)*time.Second, store, nil)
	})
	hcCtx, hcCancel := context.WithCancel(ctx)
	defer hcCancel()
	hctx, c := hcCtx, cfg
	run("health", func() { health.Run(hctx, c.Checks, c.DNS.Targets, c.DNS.Servers, store) })

	// config reload — shared by fsnotify watch and the tray menu item.
	// Serialized: both callers run on separate goroutines and reassign
	// cfg/hcCtx/hcCancel.
	var reloadMu sync.Mutex
	doReload := func() {
		reloadMu.Lock()
		defer reloadMu.Unlock()
		nc, err := config.Load(cfgPath)
		if err != nil {
			store.Emit(state.Event{Kind: state.EvConfigReloaded, Detail: "config invalid, keeping previous"})
			return
		}
		cfg = nc
		hcCancel()
		hcCtx, hcCancel = context.WithCancel(ctx)
		hctx, c := hcCtx, cfg // bind before the goroutine reads them
		run("health", func() { health.Run(hctx, c.Checks, c.DNS.Targets, c.DNS.Servers, store) })
		store.EmitConfigReloaded()
	}
	unwatch, err := config.Watch(cfgPath, func(*config.Config) { doReload() })
	if err != nil {
		log.Printf("config watch unavailable, reload only via tray: %v", err)
	} else {
		defer func() { _ = unwatch() }()
	}

	// tray (blocks); menu unavailable or died mid-run → notify-only until
	// ctx is cancelled (workers + config watch keep running).
	if err := tray.Run(ctx, store, doReload, cancel); err != nil {
		log.Printf("tray unavailable, notify-only: %v", err)
		<-ctx.Done()
	}
}

// stateLogPath returns ~/.local/state/netpulse/events.log, creating the dir.
func stateLogPath() string {
	dir := filepath.Join(os.Getenv("HOME"), ".local", "state", "netpulse")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("state dir: %v", err)
	}
	return filepath.Join(dir, "events.log")
}
