// Code generated from Pkl module `alexandria.ai.Ai`. DO NOT EDIT.
package ai

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/ai/provider"
)

// AI features: Ask the Librarian (chat with catalog tools) and Enrich
// (metadata suggestions). Calls go to the lowest-priority-number provider that
// is enabled and under its cap; a provider error or exhausted cap falls
// through to the next. Every call is priced into the ai_usage ledger.
type Ai struct {
	Enabled bool `pkl:"enabled"`

	Providers []provider.Provider `pkl:"providers"`

	// Prepended to every librarian conversation.
	SystemPrompt string `pkl:"systemPrompt"`

	// Maximum tool-call rounds per user message before the model must answer.
	MaxToolRounds uint8 `pkl:"maxToolRounds"`

	// Conversation turns kept in the prompt.
	HistoryTurns uint16 `pkl:"historyTurns"`

	// Ceiling on spend across all providers per calendar day, USD. Null = none.
	DailySpendLimitUsd *float64 `pkl:"dailySpendLimitUsd"`
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a Ai
func LoadFromPath(ctx context.Context, path string) (ret Ai, err error) {
	evaluator, err := pkl.NewEvaluator(ctx, pkl.PreconfiguredOptions)
	if err != nil {
		return ret, err
	}
	defer func() {
		cerr := evaluator.Close()
		if err == nil {
			err = cerr
		}
	}()
	ret, err = Load(ctx, evaluator, pkl.FileSource(path))
	return ret, err
}

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a Ai
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (Ai, error) {
	var ret Ai
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
