package config

import "testing"

func TestScale(t *testing.T) {
	for in, want := range map[string]float64{"": 1, "x": 1, "0": 1, "-2": 1, "5000": 1, "2.5": 2.5, "100": 100} {
		if got := scale(in); got != want {
			t.Errorf("scale(%q) = %v, want %v", in, got, want)
		}
	}
}
