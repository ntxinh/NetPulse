package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/exodia/netpulse/internal/config"
	"github.com/exodia/netpulse/internal/state"
)

func TestCounter(t *testing.T) {
	c := &counter{limit: 2}
	r, ch := c.observe("x", true, time.Millisecond, nil)
	if !r.Ok || !ch {
		t.Fatal("first ok should be reported")
	}
	_, ch = c.observe("x", true, 0, errors.New("t"))
	if ch {
		t.Fatal("first failure below threshold must not emit")
	}
	r, ch = c.observe("x", true, 0, errors.New("t"))
	if r.Ok || !ch {
		t.Fatal("second failure should emit down")
	}
	_, ch = c.observe("x", true, 0, errors.New("t"))
	if ch {
		t.Fatal("steady down must not emit")
	}
	r, ch = c.observe("x", true, time.Millisecond, nil)
	if !r.Ok || !ch {
		t.Fatal("recovery should emit ok")
	}
}

// A parent-ctx cancellation mid-probe is shutdown/reload, not a completed
// probe that fails: the counter must not increment and nothing is emitted.
func TestWorkerCancelMidProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := state.NewStore()
	a := &acc{results: make(map[string]state.Result), store: store}
	chk := config.Check{Name: "x", Type: "tcp", IntervalSecs: 60, TimeoutSecs: 60, Failures: 1}
	blocking := func(ctx context.Context, _ time.Duration) (time.Duration, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	done := make(chan struct{})
	go func() { worker(ctx, chk, blocking, a); close(done) }()
	time.Sleep(20 * time.Millisecond) // let the first tick enter the prober
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit on ctx cancel")
	}
	_, _, _, h := store.Snapshot()
	if len(h.Results) != 0 {
		t.Fatalf("cancelled probe emitted results: %+v", h.Results)
	}
}

// Exercises constructor-error write racing a live worker's commit under -race.
func TestRunRace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := state.NewStore()
	checks := []config.Check{
		// tcp first so its worker is live while Run continues;
		// 127.0.0.1:1 refuses immediately → result on first tick.
		{Name: "tcp-bad", Type: "tcp", Target: "127.0.0.1:1", IntervalSecs: 60, TimeoutSecs: 1, Failures: 1},
		{Name: "bad", Type: "nope", Target: "x", IntervalSecs: 60, TimeoutSecs: 1, Failures: 1},
	}
	Run(ctx, checks, nil, nil, store)
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, _, _, h := store.Snapshot()
		badReported := false
		for _, r := range h.Results {
			if r.Name == "bad" && strings.HasPrefix(r.Err, "prober: ") && !r.Ok {
				badReported = true
			}
		}
		if len(h.Results) >= 2 && badReported {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("want >=2 results incl. constructor error, got %+v", h.Results)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
