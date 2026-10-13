package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// openaiProvider talks to the Chat Completions API over plain HTTP: tools as
// function calls, structured output as a json_schema response format. It also
// serves OpenAI-compatible servers (Ollama, vLLM) through BaseURL.
type openaiProvider struct {
	cfg    ProviderConfig
	client *http.Client
}

func newOpenAI(cfg ProviderConfig, hc *http.Client) *openaiProvider {
	if hc == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = 120 * time.Second
		}
		hc = &http.Client{Timeout: timeout}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	return &openaiProvider{cfg: cfg, client: hc}
}

func (p *openaiProvider) Name() string  { return p.cfg.Name }
func (p *openaiProvider) Model() string { return p.cfg.Model }

// Wire shapes (only the fields we use).
type oaiMessage struct {
	Role       string        `json:"role"`
	Content    *string       `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaiRequest struct {
	Model               string         `json:"model"`
	Messages            []oaiMessage   `json:"messages"`
	Tools               []any          `json:"tools,omitempty"`
	MaxCompletionTokens int            `json:"max_completion_tokens,omitempty"`
	ResponseFormat      map[string]any `json:"response_format,omitempty"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
}

type oaiResponse struct {
	Choices []struct {
		Message      oaiMessage `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete runs one Chat Completions call. It never loops: the chat service
// owns the tool rounds so it can log and price each one.
func (p *openaiProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	body, err := json.Marshal(p.buildRequest(req))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
	res, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai %s: %w", p.cfg.Name, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	var out oaiResponse
	if jerr := json.Unmarshal(raw, &out); jerr != nil && res.StatusCode == http.StatusOK {
		return nil, fmt.Errorf("openai %s: decode: %w", p.cfg.Name, jerr)
	}
	if res.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if out.Error != nil {
			msg = out.Error.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("openai %s: status %d: %s", p.cfg.Name, res.StatusCode, msg)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("openai %s: no choices", p.cfg.Name)
	}
	return translateOpenAI(out), nil
}

func (p *openaiProvider) buildRequest(req Request) oaiRequest {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.cfg.MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	out := oaiRequest{Model: p.cfg.Model, MaxCompletionTokens: maxTokens, ReasoningEffort: p.cfg.Effort}
	if req.System != "" {
		s := req.System
		out.Messages = append(out.Messages, oaiMessage{Role: "system", Content: &s})
	}
	for _, turn := range req.Messages {
		out.Messages = append(out.Messages, openaiMessages(turn)...)
	}
	for _, t := range req.Tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out.Tools = append(out.Tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  schema,
				"strict":      strictOK(schema),
			},
		})
	}
	if req.OutputSchema != nil {
		out.ResponseFormat = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "output",
				"schema": req.OutputSchema,
				"strict": strictOK(req.OutputSchema),
			},
		}
	}
	return out
}

// strictOK reports whether a schema meets OpenAI's strict-mode rules: every
// property listed in required and additionalProperties false. Schemas with
// optional properties are sent without strict rather than rejected.
func strictOK(schema map[string]any) bool {
	if ap, ok := schema["additionalProperties"].(bool); !ok || ap {
		return false
	}
	props, _ := schema["properties"].(map[string]any)
	var required []string
	switch r := schema["required"].(type) {
	case []string:
		required = r
	case []any:
		for _, v := range r {
			if s, ok := v.(string); ok {
				required = append(required, s)
			}
		}
	}
	if len(required) != len(props) {
		return false
	}
	for name := range props {
		found := false
		for _, r := range required {
			if r == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// openaiMessages converts one Turn: an assistant turn is one message with
// tool_calls; a user turn becomes one `tool` message per result followed by
// the text, which is the order the API expects.
func openaiMessages(turn Turn) []oaiMessage {
	if native, ok := turn.Native.(oaiMessage); ok {
		return []oaiMessage{native}
	}
	switch turn.Role {
	case RoleAssistant:
		m := oaiMessage{Role: "assistant"}
		if turn.Text != "" {
			t := turn.Text
			m.Content = &t
		}
		for _, tu := range turn.ToolUses {
			var tc oaiToolCall
			tc.ID, tc.Type = tu.ID, "function"
			tc.Function.Name = tu.Name
			tc.Function.Arguments = string(tu.Input)
			if tc.Function.Arguments == "" {
				tc.Function.Arguments = "{}"
			}
			m.ToolCalls = append(m.ToolCalls, tc)
		}
		if m.Content == nil && len(m.ToolCalls) == 0 {
			empty := "(no reply)"
			m.Content = &empty
		}
		return []oaiMessage{m}
	default:
		var out []oaiMessage
		for _, tr := range turn.ToolResults {
			content := tr.Output
			if tr.IsError {
				content = "Error: " + content
			}
			c := content
			out = append(out, oaiMessage{Role: "tool", ToolCallID: tr.ID, Content: &c})
		}
		if turn.Text != "" || len(out) == 0 {
			t := turn.Text
			if t == "" {
				t = "(empty)"
			}
			out = append(out, oaiMessage{Role: "user", Content: &t})
		}
		return out
	}
}

func translateOpenAI(out oaiResponse) *Response {
	choice := out.Choices[0]
	resp := &Response{
		Usage: Usage{
			InputTokens:     out.Usage.PromptTokens,
			OutputTokens:    out.Usage.CompletionTokens,
			CacheReadTokens: out.Usage.PromptTokensDetails.CachedTokens,
		},
		Native: choice.Message,
	}
	if choice.Message.Content != nil {
		resp.Text = *choice.Message.Content
	}
	for _, tc := range choice.Message.ToolCalls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			args = "{}"
		}
		resp.ToolUses = append(resp.ToolUses, ToolUse{ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage(args)})
	}
	switch choice.FinishReason {
	case "tool_calls", "function_call":
		resp.StopReason = StopToolUse
	case "length":
		resp.StopReason = StopMaxTokens
	case "content_filter":
		resp.StopReason = StopRefusal
	default:
		resp.StopReason = StopEndTurn
		if len(resp.ToolUses) > 0 {
			resp.StopReason = StopToolUse
		}
	}
	return resp
}
