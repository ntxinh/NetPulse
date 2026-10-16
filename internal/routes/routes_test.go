package routes

import "testing"

var table = `[{"dst":"default","gateway":"192.168.1.1"},{"dst":"192.168.1.0/24","dev":"enp1s0"}]`

func TestMissing(t *testing.T) {
	m := Missing([]byte(table), []string{"default", "10.0.0.0/8"})
	if len(m) != 1 || m[0] != "10.0.0.0/8" {
		t.Fatalf("%v", m)
	}
	m = Missing([]byte(`[]`), []string{"default"})
	if len(m) != 1 || m[0] != "default" {
		t.Fatal("empty table = default missing")
	}
}

func TestMissingBadJSON(t *testing.T) {
	m := Missing([]byte(`not json`), []string{"default"})
	if len(m) != 1 || m[0] != "default" {
		t.Fatalf("unparsable table should count everything missing: %v", m)
	}
}
