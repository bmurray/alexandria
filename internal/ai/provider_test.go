package ai

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestAnthropicBuildParams(t *testing.T) {
	p := newAnthropic(ProviderConfig{Name: "opus", Type: "anthropic", Model: "claude-opus-5-5", APIKey: "k", Effort: "low", MaxTokens: 2048, Timeout: time.Second, BaseURL: "http://localhost:1"}, nil)
	req := Request{
		System: "be nice",
		Tools:  toolset(NewFakeCatalog(), nil)[:2],
		Messages: []Turn{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Text: "looking", ToolUses: []ToolUse{{ID: "tu1", Name: "search_books", Input: json.RawMessage(`{"query":"x"}`)}}},
			{Role: RoleUser, ToolResults: []ToolResult{{ID: "tu1", Output: `{"books":[]}`}, {ID: "tu2", Output: "boom", IsError: true}}},
			{Role: RoleAssistant},
			{Role: RoleUser},
		},
		OutputSchema: enrichSchema,
	}
	raw, err := json.Marshal(p.buildParams(req))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "claude-opus-5-5" || got["max_tokens"] != float64(2048) {
		t.Fatalf("model/max_tokens: %v", got)
	}
	if _, has := got["thinking"]; has {
		t.Fatal("thinking must be left unset")
	}
	if _, has := got["tool_choice"]; has {
		t.Fatal("tool_choice must not be forced")
	}
	oc := got["output_config"].(map[string]any)
	if oc["effort"] != "low" || oc["format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("output_config: %v", oc)
	}
	if sys := got["system"].([]any)[0].(map[string]any); sys["text"] != "be nice" {
		t.Fatalf("system: %v", sys)
	}
	tools := got["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools: %v", tools)
	}
	search := tools[0].(map[string]any)
	schema := search["input_schema"].(map[string]any)
	if search["name"] != "search_books" || search["strict"] != true || schema["additionalProperties"] != false || schema["type"] != "object" {
		t.Fatalf("tool: %s", raw)
	}
	if req := schema["required"].([]any); len(req) != 1 || req[0] != "query" {
		t.Fatalf("required: %v", schema)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 5 {
		t.Fatalf("messages: %d", len(msgs))
	}
	assistant := msgs[1].(map[string]any)
	blocks := assistant["content"].([]any)
	if assistant["role"] != "assistant" || len(blocks) != 2 || blocks[1].(map[string]any)["type"] != "tool_use" || blocks[1].(map[string]any)["input"].(map[string]any)["query"] != "x" {
		t.Fatalf("assistant turn: %v", assistant)
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if len(results) != 2 || results[0].(map[string]any)["tool_use_id"] != "tu1" || results[1].(map[string]any)["is_error"] != true {
		t.Fatalf("tool results: %v", results)
	}
	if !strings.Contains(string(raw), "(no reply)") || !strings.Contains(string(raw), "(empty)") {
		t.Fatal("empty turns need placeholder text")
	}
}

func TestAnthropicDefaultsAndNative(t *testing.T) {
	p := newAnthropic(ProviderConfig{Name: "h", Model: "claude-haiku-5-5"}, nil)
	if p.Name() != "h" || p.Model() != "claude-haiku-5-5" {
		t.Fatal("accessors")
	}
	params := p.buildParams(Request{MaxTokens: 0})
	if params.MaxTokens != defaultMaxTokens || len(params.System) != 0 {
		t.Fatalf("%+v", params)
	}
	// A native SDK param is passed through untouched.
	native := anthropic.NewAssistantMessage(anthropic.NewTextBlock("verbatim"))
	params = p.buildParams(Request{Messages: []Turn{{Role: RoleAssistant, Text: "ignored", Native: native}}})
	raw, _ := json.Marshal(params.Messages[0])
	if !strings.Contains(string(raw), "verbatim") || strings.Contains(string(raw), "ignored") {
		t.Fatal(string(raw))
	}
}

func TestTranslateResponse(t *testing.T) {
	var msg anthropic.Message
	err := json.Unmarshal([]byte(`{
	  "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
	  "content": [
	    {"type": "text", "text": "Let me look."},
	    {"type": "tool_use", "id": "toolu_1", "name": "search_books", "input": {"query": "dune", "limit": 3}},
	    {"type": "text", "text": "and more"}
	  ],
	  "stop_reason": "tool_use", "stop_sequence": null,
	  "usage": {"input_tokens": 12, "output_tokens": 34, "cache_read_input_tokens": 5, "cache_creation_input_tokens": 0}
	}`), &msg)
	if err != nil {
		t.Fatal(err)
	}
	resp := translateResponse(&msg)
	if resp.StopReason != StopToolUse || resp.Text != "Let me look.\nand more" || resp.Usage != (Usage{12, 34, 5}) {
		t.Fatalf("%+v", resp)
	}
	if len(resp.ToolUses) != 1 || resp.ToolUses[0].ID != "toolu_1" || string(resp.ToolUses[0].Input) != `{"query": "dune", "limit": 3}` {
		t.Fatalf("%+v", resp.ToolUses)
	}
	if _, ok := resp.Native.(anthropic.MessageParam); !ok {
		t.Fatal("native param missing")
	}

	var refusal anthropic.Message
	json.Unmarshal([]byte(`{"id":"m","type":"message","role":"assistant","content":[],"stop_reason":"refusal","stop_details":{"category":"cyber","explanation":"policy"},"usage":{"input_tokens":1,"output_tokens":0}}`), &refusal)
	r := translateResponse(&refusal)
	if r.StopReason != StopRefusal || r.Text != "policy" {
		t.Fatalf("%+v", r)
	}
}

func TestNewProvider(t *testing.T) {
	if _, err := NewProvider(ProviderConfig{Type: "ANTHROPIC"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := NewProvider(ProviderConfig{Type: "ollama"}, nil); err == nil {
		t.Fatal("want error")
	}
}
