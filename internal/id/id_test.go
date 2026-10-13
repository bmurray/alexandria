package id

import "testing"

func TestNew(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		s := New()
		if len(s) != 20 {
			t.Fatalf("len(%q) = %d", s, len(s))
		}
		if seen[s] {
			t.Fatalf("duplicate id %q", s)
		}
		seen[s] = true
	}
	if len(Token(32)) < 40 {
		t.Fatal("token too short")
	}
}
