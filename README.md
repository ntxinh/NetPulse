# NetPulse

System-tray network monitor for Fedora/GNOME: watches interfaces, routes,
NetworkManager VPNs, and Tailscale; runs configurable health checks; and
fires desktop notifications on state transitions. Green/amber/red icon
reflects worst-case health at a glance.

## Build

```bash
go build -o netpulse ./cmd/netpulse
```

## Run

```bash
./netpulse
```

Click the tray icon for live state. `SIGINT`/`SIGTERM` exits cleanly.

## Configuration

`~/.config/netpulse/netpulse.toml` — created with defaults and comments on
first run, watched via fsnotify, and reloaded without restart (tray menu
has a manual "Reload config" too). Invalid TOML keeps the last-good config
and sends one notification.

```toml
[general]
iface_interval_secs = 1
tailscale_interval_secs = 5
log_events = true
# Route prefixes that must stay present — alert if any disappear.
# Default route presence is always monitored; add internal nets here.
routes = []

[dns]
# Defaults for type="dns" checks: names to resolve and which servers.
targets = ["one.one.one.one"]   # FQDNs to resolve
servers = []                    # empty = system resolver

[[check]]
name = "router"
type = "icmp"                   # icmp | tcp | http | dns
target = "192.168.1.1"
interval_secs = 10
timeout_secs = 3
failures = 2                    # consecutive misses before "down"
critical = true                 # false = amber-only, never red icon

[[check]]
name = "corp vpn endpoint"
type = "tcp"
target = "10.0.0.5:443"
interval_secs = 15
```

A `type = "dns"` check resolves each name in `dns.targets` via `dns.servers`
(or the system resolver when empty); `target` is ignored for that type.

## Tray

The icon is a three-state dot: **green** all ok, **amber** degraded
(non-critical check down or a data source unavailable), **red** breakage
(VPN dropped, Tailscale disconnected, watched/default route missing,
critical check down). The menu is rebuilt on every state change and shows
interfaces with IPs and throughput, VPN/Tailscale status and peers, per-check
results with latency, and Reload/Quit actions.

## Notifications

Every transition — route missing/restored, VPN change, check down/up,
config reload — produces a desktop notification via the freedesktop
org.freedesktop.Notifications service. If no session bus is available
NetPulse still runs (tray + optional log only). With `log_events = true`
each event is also appended to `~/.local/state/netpulse/events.log`.

## Fedora notes

- `tailscale` and `nmcli` must be on `PATH` for Tailscale and VPN state;
  when absent those sources report `unavailable` (amber) and the rest keeps
  working.
- ICMP checks use unprivileged ping sockets (`ping_group_range`), which
  Fedora enables by default — no setuid or capabilities needed.
- Requires a StatusNotifierItem-capable shell/panel for the tray icon; in a
  tray-less environment it runs notify-only and logs a warning.
