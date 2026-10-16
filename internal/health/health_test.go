package health

import (
	"errors"
	"testing"
	"time"
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
