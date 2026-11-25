package main

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.0.12", "0.0.9", true}, {"v0.0.9", "0.0.12", false}, {"0.0.5", "0.0.5", false}, {"0.1.0", "0.0.99", true}} {
		if newer(c.a, c.b) != c.want {
			t.Errorf("newer(%s,%s) != %v", c.a, c.b, c.want)
		}
	}
}
