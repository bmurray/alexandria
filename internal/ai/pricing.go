package ai

import "math"

// Pricing is a provider's list price in US dollars per million tokens.
type Pricing struct {
	InputPerM     float64
	OutputPerM    float64
	CacheReadPerM float64
}

// Usage is the token count of one model call.
type Usage struct {
	InputTokens     int `json:"inputTokens"`
	OutputTokens    int `json:"outputTokens"`
	CacheReadTokens int `json:"cacheReadTokens"`
}

// Add returns the sum of two usages.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:     u.InputTokens + o.InputTokens,
		OutputTokens:    u.OutputTokens + o.OutputTokens,
		CacheReadTokens: u.CacheReadTokens + o.CacheReadTokens,
	}
}

// Cost prices a usage, rounded to the nearest micro-dollar so that ledger sums
// stay stable. Negative token counts are treated as zero.
func (p Pricing) Cost(u Usage) float64 {
	usd := float64(max(u.InputTokens, 0))*p.InputPerM +
		float64(max(u.OutputTokens, 0))*p.OutputPerM +
		float64(max(u.CacheReadTokens, 0))*p.CacheReadPerM
	return RoundMicro(usd / 1e6)
}

// RoundMicro rounds a dollar amount to six decimals.
func RoundMicro(usd float64) float64 {
	return math.Round(usd*1e6) / 1e6
}
