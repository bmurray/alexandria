// Code generated from Pkl module `alexandria.ai.provider.OpenAI`. DO NOT EDIT.
package openai

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/ai/provider"
	"github.com/bmurray/alexandria/internal/gen/credential"
)

type OpenAI interface {
	provider.Provider

	GetBaseUrl() string

	GetReasoningEffort() *string
}

var _ OpenAI = OpenAIImpl{}

// OpenAI Chat Completions API (also any compatible server such as Ollama or
// vLLM via `baseUrl`). Tools and structured JSON output are supported.
type OpenAIImpl struct {
	Type string `pkl:"type"`

	// Base URL without the path; "/chat/completions" is appended.
	BaseUrl string `pkl:"baseUrl"`

	// `reasoning_effort`. Null omits the field. Note gpt-6-luna only accepts
	// function tools on Chat Completions with "none".
	ReasoningEffort *string `pkl:"reasoningEffort"`

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

func (rcv OpenAIImpl) GetType() string {
	return rcv.Type
}

// Base URL without the path; "/chat/completions" is appended.
func (rcv OpenAIImpl) GetBaseUrl() string {
	return rcv.BaseUrl
}

// `reasoning_effort`. Null omits the field. Note gpt-6-luna only accepts
// function tools on Chat Completions with "none".
func (rcv OpenAIImpl) GetReasoningEffort() *string {
	return rcv.ReasoningEffort
}

// Unique name used in the API, metrics and the database.
func (rcv OpenAIImpl) GetName() string {
	return rcv.Name
}

// Model identifier sent to the endpoint.
func (rcv OpenAIImpl) GetModel() string {
	return rcv.Model
}

func (rcv OpenAIImpl) GetApiKey() credential.Credential {
	return rcv.ApiKey
}

// Price per million input tokens, USD.
func (rcv OpenAIImpl) GetInputCostPerMillionUsd() float64 {
	return rcv.InputCostPerMillionUsd
}

// Price per million output tokens, USD.
func (rcv OpenAIImpl) GetOutputCostPerMillionUsd() float64 {
	return rcv.OutputCostPerMillionUsd
}

// Price per million prompt-cache read tokens, USD.
func (rcv OpenAIImpl) GetCacheReadCostPerMillionUsd() float64 {
	return rcv.CacheReadCostPerMillionUsd
}

// Maximum output tokens per request.
func (rcv OpenAIImpl) GetMaxTokens() uint32 {
	return rcv.MaxTokens
}

// Lower runs first. The cheapest fallback should have the highest number.
func (rcv OpenAIImpl) GetPriority() uint16 {
	return rcv.Priority
}

// Initial spend cap in USD; null = unlimited.
func (rcv OpenAIImpl) GetSpendLimitUsd() *float64 {
	return rcv.SpendLimitUsd
}

func (rcv OpenAIImpl) GetEnabled() bool {
	return rcv.Enabled
}

func (rcv OpenAIImpl) GetTimeoutSeconds() uint16 {
	return rcv.TimeoutSeconds
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a OpenAI
func LoadFromPath(ctx context.Context, path string) (ret OpenAI, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a OpenAI
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (OpenAI, error) {
	var ret OpenAIImpl
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
