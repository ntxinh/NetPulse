package tailscale

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/exodia/netpulse/internal/state"
)

func TestParseRunning(t *testing.T) {
	data, _ := os.ReadFile("testdata/running.json")
	s, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if s.Backend != "Running" || s.SelfIP != "100.64.1.5" {
		t.Fatalf("%+v", s)
	}
	if len(s.Peers) != 2 {
		t.Fatalf("peers: %+v", s.Peers)
	}
	// sorted by name for stable menu
	if s.Peers[0].Name != "laptop" || s.Peers[1].Name != "nas" {
		t.Fatal("peers must be sorted by name")
	}
	if !s.Peers[1].Exit {
		t.Fatal("nas must be marked exit-capable")
	}
}

func TestParseStopped(t *testing.T) {
	data, _ := os.ReadFile("testdata/stopped.json")
	s, _ := Parse(data)
	if s.Backend != "Stopped" || len(s.Peers) != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestParseExitNode(t *testing.T) {
	data, _ := os.ReadFile("testdata/exitnode.json")
	s, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if s.ExitNode != "100.64.1.10" {
		t.Fatalf("ExitNode: %q", s.ExitNode)
	}
}

func TestPollCommitsSnapshot(t *testing.T) {
	store := state.NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	execFn := func(ctx context.Context) ([]byte, error) {
		cancel() // first poll is enough
		return []byte(`{"BackendState":"Running","Self":{"TailscaleIPs":["100.64.1.5"]}}`), nil
	}
	Poll(ctx, time.Hour, store, execFn)
	_, _, ts, _ := store.Snapshot()
	if ts.Backend != "Running" || ts.SelfIP != "100.64.1.5" {
		t.Fatalf("%+v", ts)
	}
}

func TestPollBinaryMissing(t *testing.T) {
	store := state.NewStore()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	execFn := func(ctx context.Context) ([]byte, error) {
		calls++
		cancel()
		return nil, exec.ErrNotFound
	}
	Poll(ctx, time.Hour, store, execFn)
	_, _, ts, _ := store.Snapshot()
	if ts.Backend != "unavailable" {
		t.Fatalf("%+v", ts)
	}
}
