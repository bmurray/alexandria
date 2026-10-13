package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestProviderStateExceededEligible(t *testing.T) {
	cases := []struct {
		name               string
		s                  ProviderState
		exceeded, eligible bool
	}{
		{"no cap", ProviderState{Enabled: true, SpentUSD: 1e9}, false, true},
		{"under cap", ProviderState{Enabled: true, SpendLimitUSD: f(10), SpentUSD: 9.99}, false, true},
		{"at cap", ProviderState{Enabled: true, SpendLimitUSD: f(10), SpentUSD: 10}, true, false},
		{"over cap", ProviderState{Enabled: true, SpendLimitUSD: f(10), SpentUSD: 11}, true, false},
		{"disabled", ProviderState{Enabled: false}, false, false},
		{"zero cap", ProviderState{Enabled: true, SpendLimitUSD: f(0)}, true, false},
	}
	for _, c := range cases {
		if got := c.s.Exceeded(); got != c.exceeded {
			t.Errorf("%s: Exceeded = %v", c.name, got)
		}
		if got := c.s.Eligible(); got != c.eligible {
			t.Errorf("%s: Eligible = %v", c.name, got)
		}
	}
}

func TestOrder(t *testing.T) {
	states := []ProviderState{
		{Name: "haiku", Enabled: true, Priority: 20},
		{Name: "opus", Enabled: true, Priority: 10, SpendLimitUSD: f(5), SpentUSD: 1},
		{Name: "capped", Enabled: true, Priority: 1, SpendLimitUSD: f(1), SpentUSD: 1},
		{Name: "off", Enabled: false, Priority: 0},
		{Name: "b-tie", Enabled: true, Priority: 20},
		{Name: "a-tie", Enabled: true, Priority: 20},
	}
	got := strings.Join(Order(states, nil), ",")
	if got != "opus,a-tie,b-tie,haiku" {
		t.Fatalf("Order = %s", got)
	}
	got = strings.Join(Order(states, map[string]bool{"opus": true}), ",")
	if got != "a-tie,b-tie,haiku" {
		t.Fatalf("Order with skip = %s", got)
	}
	if n := Order(nil, nil); len(n) != 0 {
		t.Fatal("empty input should give empty order")
	}
}

func TestCharge(t *testing.T) {
	s := ProviderState{Enabled: true, SpendLimitUSD: f(1)}
	if Charge(&s, 0.4) {
		t.Fatal("0.4 should not cross")
	}
	if Charge(&s, 0.59) {
		t.Fatal("0.99 should not cross")
	}
	if !Charge(&s, 0.011) {
		t.Fatal("crossing to >= 1 should report")
	}
	if Charge(&s, 1) {
		t.Fatal("already exceeded must not report again")
	}
	if s.SpentUSD != 2.001 {
		t.Fatalf("spent = %v", s.SpentUSD)
	}
	uncapped := ProviderState{Enabled: true}
	if Charge(&uncapped, 1e6) {
		t.Fatal("no cap never crosses")
	}
}

func TestProviderStateJSON(t *testing.T) {
	b, err := json.Marshal(ProviderState{Name: "x", Enabled: true, SpendLimitUSD: f(1), SpentUSD: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"exceeded":true`) || !strings.Contains(string(b), `"spendLimitUsd":1`) {
		t.Fatal(string(b))
	}
	b, _ = json.Marshal(ProviderState{Name: "y"})
	if !strings.Contains(string(b), `"spendLimitUsd":null`) || !strings.Contains(string(b), `"exceeded":false`) {
		t.Fatal(string(b))
	}
}
