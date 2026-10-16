// Package config loads, defaults, and watches the netpulse TOML config.
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/fsnotify/fsnotify"
)

type Check struct {
	Name         string `toml:"name"`
	Type         string `toml:"type"` // icmp|tcp|http|dns
	Target       string `toml:"target"`
	IntervalSecs int    `toml:"interval_secs"`
	TimeoutSecs  int    `toml:"timeout_secs"`
	Failures     int    `toml:"failures"`
	Critical     *bool  `toml:"critical"` // nil = default true
}

// IsCritical reports whether the check can drive the red (down) icon.
func (c Check) IsCritical() bool { return c.Critical == nil || *c.Critical }

type Config struct {
	General struct {
		IfaceIntervalSecs     int      `toml:"iface_interval_secs"`
		TailscaleIntervalSecs int      `toml:"tailscale_interval_secs"`
		LogEvents             bool     `toml:"log_events"`
		Routes                []string `toml:"routes"` // extra watched prefixes
	} `toml:"general"`
	DNS struct {
		Targets []string `toml:"targets"`
		Servers []string `toml:"servers"`
	} `toml:"dns"`
	Checks []Check `toml:"check"`
}

// Path returns the default config path, ~/.config/netpulse/netpulse.toml.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "netpulse", "netpulse.toml")
}

// Load decodes path and applies spec §4 defaults to unset fields.
func Load(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, err
	}
	if c.General.IfaceIntervalSecs <= 0 {
		c.General.IfaceIntervalSecs = 1
	}
	if c.General.TailscaleIntervalSecs <= 0 {
		c.General.TailscaleIntervalSecs = 5
	}
	for i := range c.Checks {
		ch := &c.Checks[i]
		if ch.IntervalSecs <= 0 {
			ch.IntervalSecs = 10
		}
		if ch.TimeoutSecs <= 0 {
			ch.TimeoutSecs = 3
		}
		if ch.Failures <= 0 {
			ch.Failures = 2
		}
	}
	return &c, nil
}

// defaultFile is the spec §4 example, verbatim with comments.
const defaultFile = `[general]
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
`

// Ensure writes the commented default config if path does not exist,
// creating parent directories. Existing files are left untouched.
func Ensure(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	defer f.Close()
	_, err = f.WriteString(defaultFile)
	return err
}

// Watch reloads path on change. On Write|Create events the file is loaded
// after a 200ms debounce; parse success calls reload(c), failure calls
// reload(nil) so the caller can keep the last-good config.
// The returned func stops watching.
func Watch(path string, reload func(*Config)) (func() error, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	// Watch the parent dir, not the file, so atomic-rename saves (Create) work.
	if err := w.Add(filepath.Dir(path)); err != nil {
		w.Close()
		return nil, err
	}
	want := filepath.Clean(path)
	go func() {
		var t *time.Timer
		stopTimer := func() {
			if t != nil {
				t.Stop()
			}
		}
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					stopTimer()
					return
				}
				if filepath.Clean(ev.Name) != want ||
					ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				if t == nil {
					t = time.AfterFunc(200*time.Millisecond, func() {
						if c, err := Load(path); err == nil {
							reload(c)
						} else {
							reload(nil)
						}
					})
				} else {
					t.Reset(200 * time.Millisecond)
				}
			case _, ok := <-w.Errors:
				if !ok {
					stopTimer()
					return
				}
			}
		}
	}()
	return w.Close, nil
}
