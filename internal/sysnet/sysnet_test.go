package sysnet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanDelta(t *testing.T) {
	// Work on a copy: the test mutates counters.
	root := filepath.Join(t.TempDir(), "sys")
	if err := os.CopyFS(root, os.DirFS("testdata/sys")); err != nil {
		t.Fatal(err)
	}
	rxFile := filepath.Join(root, "class/net/enp1s0/statistics/rx_bytes")
	addrs := map[string]string{"enp1s0": "192.168.1.20"}

	snap, prev := Scan(root, addrs, nil, 0)
	if snap.Err != nil {
		t.Fatalf("scan: %v", snap.Err)
	}
	if len(snap.Ifaces) != 1 || snap.Ifaces[0].Name != "enp1s0" {
		t.Fatalf("%+v", snap)
	}
	iface := snap.Ifaces[0]
	if iface.RxBps != 0 || iface.TxBps != 0 {
		t.Fatal("first scan has no delta")
	}
	if !iface.Up || iface.MTU != 1500 || iface.IPv4 != "192.168.1.20" {
		t.Fatalf("%+v", iface)
	}

	// 60000 bytes over 10s → 6000 Bps.
	if err := os.WriteFile(rxFile, []byte("61000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, prev = Scan(root, addrs, prev, 10*time.Second)
	if got := snap.Ifaces[0].RxBps; got != 6000 {
		t.Fatalf("want 6000 Bps, got %d", got)
	}

	// Counter reset/wrap → 0, not a huge number.
	if err := os.WriteFile(rxFile, []byte("100\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, _ = Scan(root, addrs, prev, 10*time.Second)
	if got := snap.Ifaces[0].RxBps; got != 0 {
		t.Fatalf("counter reset: want 0 Bps, got %d", got)
	}
}
