# NetPulse — Network & VPN Monitor

## Overview

A lightweight system tray / desktop utility designed for Linux machines that frequently use multiple network interfaces, VPNs, and remote-access tools (especially Tailscale). Provides real-time visibility into network health, VPN status, routing, and connectivity to critical hosts, with timely alerts when something important breaks.

## Core Features

### Interface & Traffic Monitoring
- List and monitor all network interfaces (wired, wireless, virtual, VPN tunnels).
- Show current IP addresses (IPv4 / IPv6), link status, and MTU.
- Real-time upload / download throughput and total traffic counters per interface.

### VPN & Tailscale Status
- Detect and display status of common VPN solutions (OpenVPN, WireGuard, NetworkManager VPN connections, etc.).
- Native Tailscale integration:
  - Connection status
  - Current Tailscale IP
  - Peer / device list (optional summary view)
  - Exit node status (if used)

### Connectivity Checks
- DNS resolution health (system resolvers + optional custom DNS targets).
- Ability to reach internal / private networks (configurable test targets).
- Endpoint reachability tests (HTTP/HTTPS, TCP ports, or simple ICMP).

### Configurable Health Checks
- User-defined list of hosts / endpoints to ping or probe.
- Support for ICMP ping, TCP connect, or HTTP(S) status checks.
- Configurable interval, timeout, and failure threshold.

### Alerts & Notifications
- Immediate notification when:
  - A monitored VPN connection drops.
  - Tailscale becomes disconnected or loses peers.
  - An important route disappears.
  - A configured health-check host becomes unreachable.
- Desktop notifications (via freedesktop.org Notifications) + optional system tray icon color / badge change.
- Optional logging of events for later review.

## Technical Stack

- **Language**: Go or Rust (prefer the one that offers the best balance of low resource usage, reliable D-Bus integration, and easy packaging for Fedora).
- **System Integration**:
  - NetworkManager via D-Bus for interface, connection, and VPN status.
  - Direct use of relevant CLIs (`tailscale`, `wg`, `ip`, `nmcli`, etc.) when D-Bus is insufficient or unavailable.
  - Standard Linux networking interfaces (`/sys/class/net`, netlink, etc.) for traffic counters and route monitoring.
- **UI**: System tray icon + lightweight popup / window (GTK4, Spint, or equivalent modern toolkit that works well under Wayland).
- **Storage**: Local configuration only (TOML / YAML / JSON). No database. No cloud component.

## Target Environment

- **OS**: Fedora Workstation 44
- **Display Server**: Wayland
- **Compositor**: Niri
- **Desktop Components**: DankMaterialShell
- **Shell**: Fish

The application must function reliably under Wayland + Niri, respect system theming, and integrate cleanly with the notification and system-tray mechanisms available on this stack.

## Non-Goals

- Full-featured network diagnostic suite (packet capture, deep inspection, etc.).
- Multi-machine / multi-user management dashboard.
- Cloud synchronization or remote monitoring of other devices.
- Replacing dedicated tools such as `nmtui`, `tailscale` CLI, or Wireshark.
- Any mandatory network service or privileged daemon beyond normal user-level access + PolicyKit where required.

## Success Criteria

- Accurate real-time display of interface status, IPs, and throughput with low CPU / memory overhead.
- Reliable detection of VPN and Tailscale state changes within a few seconds.
- Configurable health checks that correctly report reachability and trigger notifications.
- Clean integration with NetworkManager D-Bus and relevant CLIs on Fedora.
- Works smoothly under the specified Wayland + Niri environment.
