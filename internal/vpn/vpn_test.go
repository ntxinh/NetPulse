package vpn

import "testing"

func TestParseActive(t *testing.T) {
	out := "corp:vpn:tun0\nhome:802-3-ethernet:enp1s0\nwg0:wireguard:wg0\n"
	got := ParseActive(out)
	if len(got) != 2 || got[0] != "corp" || got[1] != "wg0" {
		t.Fatalf("%v", got)
	}
}
