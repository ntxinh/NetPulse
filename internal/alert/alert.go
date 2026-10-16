// Package alert fans state events out to user-visible backends: a
// freedesktop desktop notification and an append-only event log.
package alert

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/exodia/netpulse/internal/state"
	"github.com/godbus/dbus/v5"
)

// Alerter consumes emitted state events.
type Alerter interface {
	Alert(e state.Event)
}

var kindString = map[state.EventKind]string{
	state.EvCheckDown:         "check-down",
	state.EvCheckUp:           "check-up",
	state.EvVpnChanged:        "vpn",
	state.EvTailscaleChanged:  "tailscale",
	state.EvRouteMissing:      "route-missing",
	state.EvRouteOk:           "route-ok",
	state.EvConfigReloaded:    "config",
	state.EvSourceUnavailable: "source-unavailable",
}

// notifyIface is the testable seam over a D-Bus object handle.
type notifyIface interface {
	Call(method string, flags dbus.Flags, args ...any) *dbus.Call
}

type notify struct {
	obj notifyIface
}

// NewNotify sends events via org.freedesktop.Notifications Notify calls.
func NewNotify(conn *dbus.Conn) Alerter {
	return newNotify(conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications"))
}

func newNotify(obj notifyIface) Alerter { return &notify{obj: obj} }

func (n *notify) Alert(e state.Event) {
	body := strings.TrimSpace(e.Subject + " " + e.Detail)
	call := n.obj.Call("org.freedesktop.Notifications.Notify", 0,
		"NetPulse", uint32(0), "", "NetPulse", body,
		[]string{}, map[string]dbus.Variant{}, int32(-1))
	if call.Err != nil {
		fmt.Fprintf(os.Stderr, "netpulse: notify failed: %v\n", call.Err)
	}
}

type logAlerter struct {
	f *os.File
}

// NewLog appends "2006-01-02T15:04:05 kind subject detail" lines to path.
// The file is created if missing and kept open for the process lifetime.
func NewLog(path string) (Alerter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &logAlerter{f: f}, nil
}

func (l *logAlerter) Alert(e state.Event) {
	kind := kindString[e.Kind]
	if kind == "" {
		kind = fmt.Sprintf("kind-%d", int(e.Kind))
	}
	line := time.Now().Format(time.RFC3339) + " " + kind + " " + e.Subject
	if e.Detail != "" {
		line += " " + e.Detail
	}
	fmt.Fprintln(l.f, line)
}

type multi struct{ as []Alerter }

// Multi fans alerts out to several backends; nil entries are skipped.
func Multi(as ...Alerter) Alerter { return &multi{as: as} }

func (m *multi) Alert(e state.Event) {
	for _, a := range m.as {
		if a != nil {
			a.Alert(e)
		}
	}
}
