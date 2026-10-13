package ai

import (
	"encoding/json"
	"sort"
)

// ProviderState is a provider's runtime row from ai_providers joined with its
// configured model. Admins change Enabled, Priority and SpendLimitUSD at
// runtime; SpentUSD accumulates from the ledger.
type ProviderState struct {
	Name          string   `json:"name"`
	Model         string   `json:"model"`
	Enabled       bool     `json:"enabled"`
	Priority      int      `json:"priority"`
	SpendLimitUSD *float64 `json:"spendLimitUsd"`
	SpentUSD      float64  `json:"spentUsd"`
}

// MarshalJSON adds the derived "exceeded" flag for API consumers.
func (s ProviderState) MarshalJSON() ([]byte, error) {
	type plain ProviderState
	return json.Marshal(struct {
		plain
		Exceeded bool `json:"exceeded"`
	}{plain(s), s.Exceeded()})
}

// Exceeded reports whether the provider has spent its cap. No cap never
// exceeds.
func (s ProviderState) Exceeded() bool {
	return s.SpendLimitUSD != nil && s.SpentUSD >= *s.SpendLimitUSD
}

// Eligible reports whether the router may send a call to this provider.
func (s ProviderState) Eligible() bool {
	return s.Enabled && !s.Exceeded()
}

// Order returns the names of eligible providers, best-first by the
// operator's priority (lower number first, name as a tiebreak), leaving out
// any in skip. The caller walks the list and falls through on error.
func Order(states []ProviderState, skip map[string]bool) []string {
	eligible := make([]ProviderState, 0, len(states))
	for _, s := range states {
		if s.Eligible() && !skip[s.Name] {
			eligible = append(eligible, s)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].Priority != eligible[j].Priority {
			return eligible[i].Priority < eligible[j].Priority
		}
		return eligible[i].Name < eligible[j].Name
	})
	names := make([]string, len(eligible))
	for i, s := range eligible {
		names[i] = s.Name
	}
	return names
}

// Charge adds usd to the state's spend and reports whether this charge
// crossed the cap, so the caller can log the transition exactly once.
func Charge(s *ProviderState, usd float64) (crossed bool) {
	before := s.Exceeded()
	s.SpentUSD = RoundMicro(s.SpentUSD + usd)
	return !before && s.Exceeded()
}
