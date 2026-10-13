package ai

import "testing"

func TestPricingCost(t *testing.T) {
	p := Pricing{InputPerM: 4, OutputPerM: 20, CacheReadPerM: 0.2}
	cases := []struct {
		u    Usage
		want float64
	}{
		{Usage{}, 0},
		{Usage{InputTokens: 1_000_000}, 4},
		{Usage{OutputTokens: 500_000}, 10},
		{Usage{CacheReadTokens: 1_000_000}, 0.2},
		{Usage{InputTokens: 1234, OutputTokens: 567, CacheReadTokens: 89}, 0.016294},
		{Usage{InputTokens: -5, OutputTokens: -5, CacheReadTokens: -5}, 0},
		{Usage{InputTokens: 1}, 0.000004},
	}
	for _, c := range cases {
		if got := p.Cost(c.u); got != c.want {
			t.Errorf("Cost(%+v) = %v want %v", c.u, got, c.want)
		}
	}
}

func TestUsageAdd(t *testing.T) {
	got := Usage{1, 2, 3}.Add(Usage{10, 20, 30})
	if got != (Usage{11, 22, 33}) {
		t.Fatalf("got %+v", got)
	}
}

func TestRoundMicro(t *testing.T) {
	if RoundMicro(0.1234567) != 0.123457 {
		t.Fatal(RoundMicro(0.1234567))
	}
}
