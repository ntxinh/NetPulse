package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "netpulse.toml")
	os.WriteFile(p, []byte(`[[check]]
name = "x"
type = "tcp"
target = "h:1"
`), 0644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	chk := c.Checks[0]
	if chk.IntervalSecs != 10 || chk.TimeoutSecs != 3 || chk.Failures != 2 {
		t.Fatalf("defaults not applied: %+v", chk)
	}
	if !chk.IsCritical() {
		t.Fatal("omitted critical should default true")
	}
	if c.General.IfaceIntervalSecs != 1 || c.General.TailscaleIntervalSecs != 5 {
		t.Fatalf("general defaults: %+v", c.General)
	}
}

func TestEnsureWritesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "netpulse.toml")
	if err := Ensure(p); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatalf("written default should parse: %v", err)
	}
	if len(c.Checks) == 0 {
		t.Fatal("default file should ship one example check")
	}
}

func TestWatchBadTomlKeepsNil(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "netpulse.toml")
	os.WriteFile(p, []byte("[general]\n"), 0644)
	got := make(chan *Config, 2)
	unwatch, err := Watch(p, func(c *Config) { got <- c })
	if err != nil {
		t.Fatal(err)
	}
	defer unwatch()
	os.WriteFile(p, []byte("not = [toml"), 0644)
	select {
	case c := <-got:
		if c != nil {
			t.Fatal("bad TOML must deliver nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reload callback")
	}
}
