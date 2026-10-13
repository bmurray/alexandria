// Code generated from Pkl module `alexandria.ai.provider.Anthropic`. DO NOT EDIT.
package anthropic

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/ai/provider"
	"github.com/bmurray/alexandria/internal/gen/credential"
)

type Anthropic interface {
	provider.Provider

	GetBaseUrl() *string

	GetEffort() string
}

var _ Anthropic = AnthropicImpl{}

// Anthropic Messages API through the official Go SDK.
type AnthropicImpl struct {
	Type string `pkl:"type"`

	// Base URL override; null uses the SDK default (https://api.anthropic.com).
	BaseUrl *string `pkl:"baseUrl"`

	// Reasoning effort passed as output_config.effort. Opus 5.5 defaults to
	// medium server-side; chat over a small catalog is fine at low.
	Effort string `pkl:"effort"`

	// Unique name used in the API, metrics and the database.
	Name string `pkl:"name"`

	// Model identifier sent to the endpoint.
	Model string `pkl:"model"`

	ApiKey credential.Credential `pkl:"apiKey"`

	// Price per million input tokens, USD.
	InputCostPerMillionUsd float64 `pkl:"inputCostPerMillionUsd"`

	// Price per million output tokens, USD.
	OutputCostPerMillionUsd float64 `pkl:"outputCostPerMillionUsd"`

	// Price per million prompt-cache read tokens, USD.
	CacheReadCostPerMillionUsd float64 `pkl:"cacheReadCostPerMillionUsd"`

	// Maximum output tokens per request.
	MaxTokens uint32 `pkl:"maxTokens"`

	// Lower runs first. The cheapest fallback should have the highest number.
	Priority uint16 `pkl:"priority"`

	// Initial spend cap in USD; null = unlimited.
	SpendLimitUsd *float64 `pkl:"spendLimitUsd"`

	Enabled bool `pkl:"enabled"`

	TimeoutSeconds uint16 `pkl:"timeoutSeconds"`
}

func (rcv AnthropicImpl) GetType() string {
	return rcv.Type
}

// Base URL override; null uses the SDK default (https://api.anthropic.com).
func (rcv AnthropicImpl) GetBaseUrl() *string {
	return rcv.BaseUrl
}

// Reasoning effort passed as output_config.effort. Opus 5.5 defaults to
// medium server-side; chat over a small catalog is fine at low.
func (rcv AnthropicImpl) GetEffort() string {
	return rcv.Effort
}

// Unique name used in the API, metrics and the database.
func (rcv AnthropicImpl) GetName() string {
	return rcv.Name
}

// Model identifier sent to the endpoint.
func (rcv AnthropicImpl) GetModel() string {
	return rcv.Model
}

func (rcv AnthropicImpl) GetApiKey() credential.Credential {
	return rcv.ApiKey
}

// Price per million input tokens, USD.
func (rcv AnthropicImpl) GetInputCostPerMillionUsd() float64 {
	return rcv.InputCostPerMillionUsd
}

// Price per million output tokens, USD.
func (rcv AnthropicImpl) GetOutputCostPerMillionUsd() float64 {
	return rcv.OutputCostPerMillionUsd
}

// Price per million prompt-cache read tokens, USD.
func (rcv AnthropicImpl) GetCacheReadCostPerMillionUsd() float64 {
	return rcv.CacheReadCostPerMillionUsd
}

// Maximum output tokens per request.
func (rcv AnthropicImpl) GetMaxTokens() uint32 {
	return rcv.MaxTokens
}

// Lower runs first. The cheapest fallback should have the highest number.
func (rcv AnthropicImpl) GetPriority() uint16 {
	return rcv.Priority
}

// Initial spend cap in USD; null = unlimited.
func (rcv AnthropicImpl) GetSpendLimitUsd() *float64 {
	return rcv.SpendLimitUsd
}

func (rcv AnthropicImpl) GetEnabled() bool {
	return rcv.Enabled
}

func (rcv AnthropicImpl) GetTimeoutSeconds() uint16 {
	return rcv.TimeoutSeconds
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a Anthropic
func LoadFromPath(ctx context.Context, path string) (ret Anthropic, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a Anthropic
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (Anthropic, error) {
	var ret AnthropicImpl
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
