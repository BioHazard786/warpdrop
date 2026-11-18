package ratelimit

import "testing"

func TestIPBurstThenDeny(t *testing.T) {
	r := NewIP(1, 3)
	for i := 0; i < 3; i++ {
		if !r.Allow("a") {
			t.Fatalf("burst %d denied", i)
		}
	}
	if r.Allow("a") {
		t.Fatal("4th immediate call must be denied")
	}
	if !r.Allow("b") {
		t.Fatal("other IP is independent")
	}
}

func TestConns(t *testing.T) {
	c := NewConns(2)
	if !c.Acquire("a") || !c.Acquire("a") || c.Acquire("a") {
		t.Fatal("cap of 2 not enforced")
	}
	c.Release("a")
	if !c.Acquire("a") {
		t.Fatal("slot not freed")
	}
}
