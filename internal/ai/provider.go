package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Role is who said a message.
type Role string

// Roles stored in ai_messages.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ToolDef is one tool the model may call. InputSchema is a JSON Schema object
// (with additionalProperties false so strict mode applies). Run receives the
// raw input and returns the text handed back to the model.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	Run         func(ctx context.Context, input json.RawMessage) (string, error)
}

// ToolCall is one executed tool call as persisted with the assistant message
// and returned to the web app.
type ToolCall struct {
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
}

// Message is what we persist per turn.
type Message struct {
	Role      Role       `json:"role"`
	Text      string     `json:"text"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
}

// ToolUse is a tool the model asked us to run.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is our answer to one ToolUse.
type ToolResult struct {
	ID      string
	Output  string
	IsError bool
}

// Turn is one provider-neutral entry of the conversation sent to the model.
// A user turn has Text and/or ToolResults; an assistant turn has Text and/or
// ToolUses. Native, when set, is the provider's own representation of the
// turn (for instance the SDK message param from a previous response, which
// carries thinking blocks the API wants back verbatim); a provider uses it
// when it recognises the type and falls back to the neutral fields otherwise.
type Turn struct {
	Role        Role
	Text        string
	ToolUses    []ToolUse
	ToolResults []ToolResult
	Native      any
}

// Request is one model call.
type Request struct {
	System    string
	Messages  []Turn
	Tools     []ToolDef
	MaxTokens int
	// OutputSchema, when set, asks for structured JSON output matching this
	// JSON Schema (object with additionalProperties false).
	OutputSchema map[string]any
}

// Response is what came back.
type Response struct {
	Text       string
	ToolUses   []ToolUse
	StopReason string
	Usage      Usage
	// Native is the provider's representation of the assistant turn; put it
	// on the Turn you append to history.
	Native any
}

// Stop reasons we act on (provider-neutral strings).
const (
	StopEndTurn   = "end_turn"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

// Provider is one configured model endpoint.
type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, req Request) (*Response, error)
}

// ErrUnsupportedProvider is returned by NewProvider for an unknown type.
var ErrUnsupportedProvider = errors.New("ai: unsupported provider type")

// NewProvider builds a Provider from config. hc may be nil.
func NewProvider(cfg ProviderConfig, hc *http.Client) (Provider, error) {
	switch strings.ToLower(cfg.Type) {
	case "anthropic":
		return newAnthropic(cfg, hc), nil
	case "openai":
		return newOpenAI(cfg, hc), nil
	default:
		return nil, fmt.Errorf("%w: %q (provider %q)", ErrUnsupportedProvider, cfg.Type, cfg.Name)
	}
}

const defaultMaxTokens = 4096

// anthropicProvider talks to the Claude Messages API through the official SDK.
type anthropicProvider struct {
	cfg    ProviderConfig
	client anthropic.Client
}

func newAnthropic(cfg ProviderConfig, hc *http.Client) *anthropicProvider {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, option.WithRequestTimeout(cfg.Timeout))
	}
	if hc != nil {
		opts = append(opts, option.WithHTTPClient(hc))
	}
	return &anthropicProvider{cfg: cfg, client: anthropic.NewClient(opts...)}
}

func (p *anthropicProvider) Name() string  { return p.cfg.Name }
func (p *anthropicProvider) Model() string { return p.cfg.Model }

// Complete runs one Messages API call. It never loops: the chat service owns
// the tool rounds so it can log and price each one.
func (p *anthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	params := p.buildParams(req)
	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic %s: %w", p.cfg.Name, err)
	}
	return translateResponse(msg), nil
}

// buildParams translates a neutral Request into SDK params. Thinking is left
// unset (Claude Opus 5.5 is always adaptive and rejects "disabled"); depth is
// steered by output_config.effort from the provider config.
func (p *anthropicProvider) buildParams(req Request) anthropic.MessageNewParams {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.cfg.MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(p.cfg.Model),
		MaxTokens: int64(maxTokens),
		Messages:  make([]anthropic.MessageParam, 0, len(req.Messages)),
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if p.cfg.Effort != "" {
		params.OutputConfig.Effort = anthropic.OutputConfigEffort(p.cfg.Effort)
	}
	if req.OutputSchema != nil {
		params.OutputConfig.Format = anthropic.JSONOutputFormatParam{Schema: req.OutputSchema}
	}
	for _, t := range req.Tools {
		params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: toolParam(t)})
	}
	for _, turn := range req.Messages {
		params.Messages = append(params.Messages, messageParam(turn))
	}
	return params
}

// toolParam converts a ToolDef. The schema's properties/required are lifted
// into the SDK struct; everything else (additionalProperties, $defs) rides
// along as extra fields so the schema reaches the API unchanged.
func toolParam(t ToolDef) *anthropic.ToolParam {
	schema := anthropic.ToolInputSchemaParam{ExtraFields: map[string]any{}}
	for k, v := range t.InputSchema {
		switch k {
		case "type":
			// always "object"
		case "properties":
			schema.Properties = v
		case "required":
			if req, ok := v.([]string); ok {
				schema.Required = req
			}
		default:
			schema.ExtraFields[k] = v
		}
	}
	if schema.Properties == nil {
		schema.Properties = map[string]any{}
	}
	if _, ok := schema.ExtraFields["additionalProperties"]; !ok {
		schema.ExtraFields["additionalProperties"] = false
	}
	return &anthropic.ToolParam{
		Name:        t.Name,
		Description: anthropic.String(t.Description),
		InputSchema: schema,
		Strict:      anthropic.Bool(true),
	}
}

// messageParam converts one Turn. Tool results go first in a user turn and
// all of a turn's results travel in that single message, as the API requires.
func messageParam(turn Turn) anthropic.MessageParam {
	if native, ok := turn.Native.(anthropic.MessageParam); ok {
		return native
	}
	var blocks []anthropic.ContentBlockParamUnion
	switch turn.Role {
	case RoleAssistant:
		if turn.Text != "" {
			blocks = append(blocks, anthropic.NewTextBlock(turn.Text))
		}
		for _, tu := range turn.ToolUses {
			blocks = append(blocks, anthropic.NewToolUseBlock(tu.ID, tu.Input, tu.Name))
		}
		if len(blocks) == 0 {
			blocks = append(blocks, anthropic.NewTextBlock("(no reply)"))
		}
		return anthropic.NewAssistantMessage(blocks...)
	default:
		for _, tr := range turn.ToolResults {
			blocks = append(blocks, anthropic.NewToolResultBlock(tr.ID, tr.Output, tr.IsError))
		}
		if turn.Text != "" {
			blocks = append(blocks, anthropic.NewTextBlock(turn.Text))
		}
		if len(blocks) == 0 {
			blocks = append(blocks, anthropic.NewTextBlock("(empty)"))
		}
		return anthropic.NewUserMessage(blocks...)
	}
}

// translateResponse flattens the SDK message. Text blocks are joined; tool
// use blocks keep their raw JSON input for the caller to decode.
func translateResponse(msg *anthropic.Message) *Response {
	resp := &Response{
		StopReason: string(msg.StopReason),
		Usage: Usage{
			InputTokens:     int(msg.Usage.InputTokens),
			OutputTokens:    int(msg.Usage.OutputTokens),
			CacheReadTokens: int(msg.Usage.CacheReadInputTokens),
		},
		Native: msg.ToParam(),
	}
	var text strings.Builder
	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(v.Text)
		case anthropic.ToolUseBlock:
			resp.ToolUses = append(resp.ToolUses, ToolUse{
				ID:    v.ID,
				Name:  v.Name,
				Input: json.RawMessage(v.JSON.Input.Raw()),
			})
		}
	}
	resp.Text = text.String()
	if resp.StopReason == StopRefusal && resp.Text == "" && msg.StopDetails.Explanation != "" {
		resp.Text = msg.StopDetails.Explanation
	}
	return resp
}
