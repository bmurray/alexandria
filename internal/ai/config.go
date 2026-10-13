// Package ai implements the model providers (Anthropic SDK), budget routing,
// the usage ledger, Ask the Librarian (chat with catalog tools) and Enrich.
package ai

import "time"

// ProviderConfig is one provider as plain values (API key already resolved).
type ProviderConfig struct {
	Name    string
	Type    string // "anthropic"
	Model   string
	APIKey  string
	BaseURL string
	Effort  string // low | medium | high

	InputPerMillionUSD     float64
	OutputPerMillionUSD    float64
	CacheReadPerMillionUSD float64

	MaxTokens     int
	Priority      int
	SpendLimitUSD *float64
	Enabled       bool
	Timeout       time.Duration
}

// Config is the AI block as plain values.
type Config struct {
	Enabled            bool
	Providers          []ProviderConfig
	SystemPrompt       string
	MaxToolRounds      int
	HistoryTurns       int
	DailySpendLimitUSD *float64
}
