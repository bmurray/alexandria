package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProvider(t *testing.T) {
	var got oaiRequest
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "bad request shape", 400)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search_books","arguments":"{\"query\":\"dune\"}"}}]}}],"usage":{"prompt_tokens":40,"completion_tokens":9,"prompt_tokens_details":{"cached_tokens":8}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Dune is on the shelf."}}],"usage":{"prompt_tokens":60,"completion_tokens":7}}`))
	}))
	defer srv.Close()

	p := newOpenAI(ProviderConfig{Name: "oai", Type: "openai", Model: "gpt-test", APIKey: "k", BaseURL: srv.URL + "/v1", Effort: "low"}, nil)
	ctx := context.Background()
	req := Request{
		System:   "You are a librarian.",
		Messages: []Turn{{Role: RoleUser, Text: "Is Dune in?"}},
		Tools: []ToolDef{{Name: "search_books", Description: "Search", InputSchema: map[string]any{
			"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}},
			"required": []string{"query"}, "additionalProperties": false,
		}}},
	}
	resp, err := p.Complete(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != StopToolUse || len(resp.ToolUses) != 1 || resp.ToolUses[0].Name != "search_books" || string(resp.ToolUses[0].Input) != `{"query":"dune"}` {
		t.Fatalf("resp %+v", resp)
	}
	if resp.Usage.InputTokens != 40 || resp.Usage.OutputTokens != 9 || resp.Usage.CacheReadTokens != 8 {
		t.Fatalf("usage %+v", resp.Usage)
	}
	if got.Messages[0].Role != "system" || got.ReasoningEffort != "low" || len(got.Tools) != 1 {
		t.Fatalf("request %+v", got)
	}
	// Optional "limit" means the tool is sent without strict.
	fn := got.Tools[0].(map[string]any)["function"].(map[string]any)
	if fn["strict"] != false {
		t.Fatalf("strict should be false for optional properties: %v", fn)
	}

	// Second round: assistant turn via Native, tool result, final text.
	req.Messages = append(req.Messages,
		Turn{Role: RoleAssistant, ToolUses: resp.ToolUses, Native: resp.Native},
		Turn{Role: RoleUser, ToolResults: []ToolResult{{ID: "call_1", Output: `[{"title":"Dune"}]`}}},
	)
	resp, err = p.Complete(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != StopEndTurn || resp.Text != "Dune is on the shelf." {
		t.Fatalf("resp %+v", resp)
	}
	if len(got.Messages) != 4 || got.Messages[2].Role != "assistant" || len(got.Messages[2].ToolCalls) != 1 || got.Messages[3].Role != "tool" || got.Messages[3].ToolCallID != "call_1" {
		t.Fatalf("history %+v", got.Messages)
	}
}

func TestOpenAIStructuredOutputAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got oaiRequest
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.ResponseFormat == nil {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"slow down","type":"rate_limit"}}`))
			return
		}
		js := got.ResponseFormat["json_schema"].(map[string]any)
		if js["strict"] != true {
			http.Error(w, "expected strict", 400)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"genre\":\"Fantasy\"}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer srv.Close()
	p := newOpenAI(ProviderConfig{Name: "oai", Model: "m", APIKey: "k", BaseURL: srv.URL}, nil)
	resp, err := p.Complete(context.Background(), Request{Messages: []Turn{{Role: RoleUser, Text: "x"}}, OutputSchema: map[string]any{
		"type": "object", "properties": map[string]any{"genre": map[string]any{"type": "string"}}, "required": []string{"genre"}, "additionalProperties": false,
	}})
	if err != nil || resp.Text != `{"genre":"Fantasy"}` {
		t.Fatalf("%+v %v", resp, err)
	}
	_, err = p.Complete(context.Background(), Request{Messages: []Turn{{Role: RoleUser, Text: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "slow down") {
		t.Fatalf("want 429 error, got %v", err)
	}
}

func TestStrictOK(t *testing.T) {
	if strictOK(map[string]any{"type": "object", "properties": map[string]any{}}) {
		t.Fatal("no additionalProperties:false must not be strict")
	}
	if !strictOK(map[string]any{"type": "object", "properties": map[string]any{"a": 1}, "required": []any{"a"}, "additionalProperties": false}) {
		t.Fatal("all required should be strict")
	}
}
