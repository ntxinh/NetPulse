package state

import "testing"

func TestCheckTransitionEmitsOnce(t *testing.T) {
	s := NewStore()
	// first commit: establishes baseline, no events
	// down: one EvCheckDown
	evs := s.CommitHealth(HealthSnap{Results: []Result{{Name: "r", Ok: false, Critical: true, Err: "timeout"}}})
	if len(evs) != 1 || evs[0].Kind != EvCheckDown || evs[0].Subject != "r" {
		t.Fatalf("want one EvCheckDown for r, got %v", evs)
	}
	// still down: no repeat
	evs = s.CommitHealth(HealthSnap{Results: []Result{{Name: "r", Ok: false, Err: "timeout"}}})
	if len(evs) != 0 {
		t.Fatalf("want no events while still down, got %v", evs)
	}
	// recovery: one EvCheckUp
	evs = s.CommitHealth(HealthSnap{Results: []Result{{Name: "r", Ok: true}}})
	if len(evs) != 1 || evs[0].Kind != EvCheckUp {
		t.Fatalf("want EvCheckUp, got %v", evs)
	}
}

func TestSeverity(t *testing.T) {
	s := NewStore()
	if s.Severity() != SevOk {
		t.Fatal("empty store should be SevOk")
	}
	s.CommitHealth(HealthSnap{Results: []Result{{Name: "a", Ok: false, Critical: false}}})
	if s.Severity() != SevDegraded {
		t.Fatal("non-critical down should be SevDegraded")
	}
	s.CommitHealth(HealthSnap{Results: []Result{{Name: "a", Ok: false, Critical: true}}})
	if s.Severity() != SevDown {
		t.Fatal("critical down should be SevDown")
	}
	s.CommitVpn(VpnSnap{Unavailable: true})
	if s.Severity() != SevDown {
		t.Fatal("vpn unavailable should keep SevDown while critical check down")
	}
}

func TestVpnDiff(t *testing.T) {
	s := NewStore()
	s.CommitVpn(VpnSnap{Active: []string{"corp"}})
	evs := s.CommitVpn(VpnSnap{Active: nil})
	if len(evs) != 1 || evs[0].Kind != EvVpnChanged || evs[0].Subject != "corp" {
		t.Fatalf("want EvVpnChanged for dropped corp, got %v", evs)
	}
}

func TestVpnDropSeverity(t *testing.T) {
	s := NewStore()
	s.CommitVpn(VpnSnap{Active: []string{"corp"}})
	s.CommitVpn(VpnSnap{Active: nil})
	if s.Severity() != SevDown {
		t.Fatal("all VPNs dropped should be SevDown")
	}
	s.CommitVpn(VpnSnap{Active: []string{"corp"}})
	if s.Severity() == SevDown {
		t.Fatal("VPN back up should clear SevDown")
	}
}
