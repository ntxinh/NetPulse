package alert

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/exodia/netpulse/internal/state"
	"github.com/godbus/dbus/v5"
)

func TestLogAppends(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.log")
	a, err := NewLog(p)
	if err != nil {
		t.Fatal(err)
	}
	a.Alert(state.Event{Kind: state.EvCheckDown, Subject: "r", Detail: "timeout"})
	a.Alert(state.Event{Kind: state.EvCheckUp, Subject: "r"})
	b, _ := os.ReadFile(p)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "check-down r timeout") {
		t.Fatalf("%q", b)
	}
	if !strings.Contains(lines[1], "check-up r") {
		t.Fatalf("%q", b)
	}
}

func TestLogKindStrings(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.log")
	a, err := NewLog(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []state.EventKind{
		state.EvRouteMissing, state.EvRouteOk, state.EvVpnChanged,
		state.EvTailscaleChanged, state.EvCheckDown, state.EvCheckUp,
		state.EvConfigReloaded, state.EvSourceUnavailable,
	} {
		a.Alert(state.Event{Kind: k, Subject: "s"})
	}
	b, _ := os.ReadFile(p)
	got := string(b)
	for _, want := range []string{
		"route-missing", "route-ok", "vpn", "tailscale",
		"check-down", "check-up", "config", "source-unavailable",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

type spy struct{ evs []state.Event }

func (s *spy) Alert(e state.Event) { s.evs = append(s.evs, e) }

func TestMultiFansOutAndSkipsNil(t *testing.T) {
	s1, s2 := &spy{}, &spy{}
	m := Multi(s1, nil, s2)
	e := state.Event{Kind: state.EvCheckDown, Subject: "x"}
	m.Alert(e)
	if len(s1.evs) != 1 || len(s2.evs) != 1 || s1.evs[0].Subject != "x" {
		t.Fatalf("s1=%v s2=%v", s1.evs, s2.evs)
	}
}

type callRec struct {
	method string
	args   []any
}

type fakeNotify struct {
	calls []callRec
	err   error
}

func (f *fakeNotify) Call(method string, flags dbus.Flags, args ...any) *dbus.Call {
	f.calls = append(f.calls, callRec{method, args})
	return &dbus.Call{Err: f.err}
}

func TestNotifyCallsDBus(t *testing.T) {
	f := &fakeNotify{}
	n := newNotify(f)
	n.Alert(state.Event{Kind: state.EvCheckDown, Subject: "r", Detail: "timeout"})
	if len(f.calls) != 1 {
		t.Fatalf("calls=%v", f.calls)
	}
	c := f.calls[0]
	if c.method != "org.freedesktop.Notifications.Notify" {
		t.Fatalf("method=%q", c.method)
	}
	// args: appName, replacesID, icon, summary, body, ...
	if c.args[0] != "NetPulse" || c.args[3] != "NetPulse" {
		t.Fatalf("args=%v", c.args)
	}
	if c.args[4] != "r timeout" {
		t.Fatalf("body=%v", c.args[4])
	}
	// best-effort: error path must not panic
	f.err = errors.New("no bus")
	n.Alert(state.Event{Kind: state.EvCheckUp, Subject: "r"})
}
