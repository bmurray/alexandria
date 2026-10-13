package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func seedBooks() *FakeCatalog {
	return NewFakeCatalog(
		BookDetail{BookSummary: BookSummary{ID: "b1", Title: "Dune", Author: "Frank Herbert", Genre: "Science fiction", Available: 0, Copies: 2, Tags: []string{"desert", "empire"}}, ISBN: "9780441013593", HoldsWaiting: 1},
		BookDetail{BookSummary: BookSummary{ID: "b2", Title: "Hyperion", Author: "Dan Simmons", Genre: "Science fiction", Available: 1, Copies: 1, Tags: []string{"pilgrims"}}},
		BookDetail{BookSummary: BookSummary{ID: "b3", Title: "Emma", Author: "Jane Austen", Genre: "Classics", Available: 3, Copies: 3}},
		BookDetail{BookSummary: BookSummary{ID: "b4", Title: "Children of Dune", Author: "Frank Herbert", Genre: "Science fiction", Available: 1, Copies: 1, Tags: []string{"desert"}}},
	)
}

func TestChatToolLoopPersistsAndCharges(t *testing.T) {
	ctx := context.Background()
	cat := seedBooks()
	p := newFake("opus",
		toolUse("search_books", `{"query":"dune"}`, 100, 20),
		text("We have Dune by Frank Herbert, but both copies are out.", 300, 40),
	)
	svc := newTestService(t, Config{MaxToolRounds: 3, HistoryTurns: 10}, cat, p)

	res, err := svc.Chat(ctx, "anon:abc", nil, "", "Do you have Dune?")
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID == "" || !strings.Contains(res.Reply, "both copies are out") {
		t.Fatalf("result: %+v", res)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "search_books" || !strings.Contains(res.ToolCalls[0].Output, `"title":"Dune"`) {
		t.Fatalf("tool calls: %+v", res.ToolCalls)
	}
	if res.Usage.InputTokens != 400 || res.Usage.OutputTokens != 60 || res.Usage.Provider != "opus" || res.Usage.Model != "opus-model" {
		t.Fatalf("usage: %+v", res.Usage)
	}
	// 400 input @ $1/M + 60 output @ $10/M.
	if res.Usage.CostUSD != 0.001 {
		t.Fatalf("cost: %v", res.Usage.CostUSD)
	}

	// Second request to the provider carried the assistant tool use and our tool result in one user turn.
	if p.calls() != 2 {
		t.Fatalf("provider calls = %d", p.calls())
	}
	second := p.Requests[1]
	if len(second.Messages) != 3 || second.Messages[1].Role != RoleAssistant || len(second.Messages[1].ToolUses) != 1 ||
		second.Messages[2].Role != RoleUser || len(second.Messages[2].ToolResults) != 1 || second.Messages[2].ToolResults[0].ID != "tu_search_books" {
		t.Fatalf("second request history: %+v", second.Messages)
	}
	if !strings.Contains(second.System, "not signed in") || !strings.Contains(second.System, "Today is") {
		t.Fatalf("system prompt: %s", second.System)
	}
	if len(second.Tools) != 6 {
		t.Fatalf("tools offered: %d", len(second.Tools))
	}

	// Persistence: title from the first message, user + assistant rows with tool calls.
	conv, msgs, err := svc.GetConversation(ctx, "anon:abc", res.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if conv.Title != "Do you have Dune?" || len(msgs) != 2 || msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant || len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("stored: %+v %+v", conv, msgs)
	}
	if _, _, err := svc.GetConversation(ctx, "someone-else", res.ConversationID); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("foreign owner must 404")
	}
	list, _ := svc.ListConversations(ctx, "anon:abc")
	if len(list) != 1 || list[0].ID != res.ConversationID {
		t.Fatalf("list: %+v", list)
	}

	// Ledger has two chat rows.
	report, _ := svc.Usage(ctx)
	if report.Totals.Calls != 2 || report.ByPurpose[0].Purpose != "chat" || report.Totals.AllTimeUSD != 0.001 {
		t.Fatalf("report: %+v", report)
	}
}

func TestChatContinuesConversationWithHistory(t *testing.T) {
	ctx := context.Background()
	p := newFake("opus", text("Hello!", 1, 1), text("Still here.", 1, 1))
	svc := newTestService(t, Config{HistoryTurns: 10}, seedBooks(), p)
	first, err := svc.Chat(ctx, "u1", &Caller{ID: "u1", Name: "Ada"}, "", "Hi")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Chat(ctx, "u1", &Caller{ID: "u1", Name: "Ada"}, first.ConversationID, "Are you there?")
	if err != nil {
		t.Fatal(err)
	}
	if second.ConversationID != first.ConversationID {
		t.Fatal("conversation id changed")
	}
	req := p.Requests[1]
	if len(req.Messages) != 3 || req.Messages[0].Text != "Hi" || req.Messages[1].Text != "Hello!" || req.Messages[2].Text != "Are you there?" {
		t.Fatalf("history replay: %+v", req.Messages)
	}
	if !strings.Contains(req.System, "signed in as Ada") {
		t.Fatal(req.System)
	}
	if _, err := svc.Chat(ctx, "u2", nil, first.ConversationID, "mine?"); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("other owner must not continue the thread")
	}
	if err := svc.DeleteConversation(ctx, "u2", first.ConversationID); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("other owner must not delete")
	}
	if err := svc.DeleteConversation(ctx, "u1", first.ConversationID); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.ListConversations(ctx, "u1"); len(list) != 0 {
		t.Fatal("conversation should be gone")
	}
}

func TestChatFallsBackToSecondProvider(t *testing.T) {
	ctx := context.Background()
	opus := newFake("opus", failing("overloaded"))
	haiku := newFake("haiku", text("Haiku here.", 5, 5))
	cfg := Config{Providers: []ProviderConfig{
		{Name: "opus", Type: "anthropic", Model: "o", Enabled: true, Priority: 10, InputPerMillionUSD: 4, OutputPerMillionUSD: 20},
		{Name: "haiku", Type: "anthropic", Model: "h", Enabled: true, Priority: 20, InputPerMillionUSD: 0.1, OutputPerMillionUSD: 0.5},
	}}
	svc := newTestService(t, cfg, seedBooks(), opus, haiku)
	res, err := svc.Chat(ctx, "u1", nil, "", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.Provider != "haiku" || res.Reply != "Haiku here." {
		t.Fatalf("got %+v", res)
	}

	// Capped first provider is skipped without being called.
	opus2 := newFake("opus", text("should not run", 1, 1))
	haiku2 := newFake("haiku", text("cheap", 1, 1))
	svc = newTestService(t, cfg, seedBooks(), opus2, haiku2)
	if _, err := svc.UpdateProvider(ctx, "opus", nil, nil, f(0), false); err != nil {
		t.Fatal(err)
	}
	res, err = svc.Chat(ctx, "u1", nil, "", "hello")
	if err != nil || res.Usage.Provider != "haiku" || opus2.calls() != 0 {
		t.Fatalf("res=%+v err=%v opus calls=%d", res, err, opus2.calls())
	}
}

func TestChatRefusalMaxRoundsAndValidation(t *testing.T) {
	ctx := context.Background()
	p := newFake("opus",
		scripted{resp: &Response{StopReason: StopRefusal, Text: "nope", Usage: Usage{InputTokens: 1}}},
		toolUse("list_genres", `{}`, 1, 1),
		toolUse("get_book", `{"id":"b1"}`, 1, 1),
		text("Final answer.", 1, 1),
	)
	svc := newTestService(t, Config{MaxToolRounds: 2}, seedBooks(), p)
	res, err := svc.Chat(ctx, "u", nil, "", "something")
	if err != nil || res.Reply != refusalReply {
		t.Fatalf("refusal: %+v %v", res, err)
	}
	// Two tool rounds, then a final call without tools.
	res, err = svc.Chat(ctx, "u", nil, "", "lots of tools")
	if err != nil || res.Reply != "Final answer." || len(res.ToolCalls) != 2 {
		t.Fatalf("rounds: %+v %v", res, err)
	}
	last := p.Requests[len(p.Requests)-1]
	if last.Tools != nil {
		t.Fatal("final round must go out without tools")
	}

	if _, err := svc.Chat(ctx, "u", nil, "", "   "); statusOf(t, err) != http.StatusBadRequest {
		t.Fatal("empty message")
	}
	if _, err := svc.Chat(ctx, "u", nil, "", strings.Repeat("x", MessageMax+1)); statusOf(t, err) != http.StatusBadRequest {
		t.Fatal("long message")
	}
	svc.cfg.Enabled = false
	if _, err := svc.Chat(ctx, "u", nil, "", "hi"); statusOf(t, err) != http.StatusServiceUnavailable {
		t.Fatal("no provider should 503")
	}
}

func TestChatToolErrorsAndUnknownToolsGoBackToModel(t *testing.T) {
	ctx := context.Background()
	p := newFake("opus",
		toolUse("teleport", `{}`, 1, 1),
		scripted{resp: &Response{StopReason: StopToolUse, Usage: Usage{InputTokens: 1},
			ToolUses: []ToolUse{{ID: "x", Name: "get_book", Input: json.RawMessage(`{"id":`)}}}},
		text("done", 1, 1),
	)
	svc := newTestService(t, Config{MaxToolRounds: 4}, seedBooks(), p)
	res, err := svc.Chat(ctx, "u", nil, "", "go")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Requests[1].Messages[2].ToolResults[0].IsError || !strings.Contains(res.ToolCalls[0].Output, "unknown tool") {
		t.Fatalf("unknown tool: %+v", res.ToolCalls)
	}
	if !p.Requests[2].Messages[4].ToolResults[0].IsError || !strings.HasPrefix(res.ToolCalls[1].Output, "tool error:") {
		t.Fatalf("bad input: %+v", res.ToolCalls)
	}
}

func TestToolsAgainstCatalog(t *testing.T) {
	ctx := context.Background()
	cat := seedBooks()
	cat.Loans["u1"] = []LoanSummary{{LoanID: "l1", BookID: "b3", Title: "Emma", Overdue: true}}
	run := func(tools []ToolDef, name, input string) string {
		t.Helper()
		for _, td := range tools {
			if td.Name == name {
				out, err := td.Run(ctx, json.RawMessage(input))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				return out
			}
		}
		t.Fatalf("tool %s missing", name)
		return ""
	}
	anon := toolset(cat, nil)
	member := toolset(cat, &Caller{ID: "u1", Name: "Ada"})

	if out := run(anon, "search_books", `{"query":"herbert","availableOnly":true,"limit":99}`); !strings.Contains(out, "Children of Dune") || strings.Contains(out, `"id":"b1"`) {
		t.Fatal(out)
	}
	if out := run(anon, "search_books", `{"query":"","genre":"Classics"}`); !strings.Contains(out, "Emma") || !strings.Contains(out, `"count":1`) {
		t.Fatal(out)
	}
	if out := run(anon, "get_book", `{"id":"b1"}`); !strings.Contains(out, `"holdsWaiting":1`) || !strings.Contains(out, "9780441013593") {
		t.Fatal(out)
	}
	if out := run(anon, "get_book", `{"id":"zz"}`); !strings.Contains(out, "error") {
		t.Fatal(out)
	}
	if out := run(anon, "list_genres", `{}`); !strings.Contains(out, `{"name":"Science fiction","count":3}`) {
		t.Fatal(out)
	}
	out := run(anon, "recommend", `{"likeBookId":"b1","interests":"austen"}`)
	if strings.Contains(out, `"id":"b1"`) && !strings.Contains(out, `"seed":{"id":"b1"`) {
		t.Fatal("seed book must not be a candidate")
	}
	for _, want := range []string{"Hyperion", "Children of Dune", "Emma", `"seed"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("recommend missing %s: %s", want, out)
		}
	}
	if out := run(anon, "recommend", `{"likeBookId":"nope"}`); !strings.Contains(out, "error") {
		t.Fatal(out)
	}
	if out := run(anon, "recommend", `{}`); out != `{"candidates":[]}` {
		t.Fatal(out)
	}
	if out := run(anon, "my_loans", `{}`); out != signInMessage {
		t.Fatal(out)
	}
	if out := run(anon, "place_hold", `{"bookId":"b1"}`); out != signInMessage {
		t.Fatal(out)
	}
	if out := run(member, "my_loans", `{}`); !strings.Contains(out, `"overdue":true`) {
		t.Fatal(out)
	}
	if out := run(member, "place_hold", `{"bookId":"b1"}`); !strings.Contains(out, `"placed":true`) || !strings.Contains(out, `"position":2`) {
		t.Fatal(out)
	}
	if out := run(member, "place_hold", `{"bookId":"b2"}`); !strings.Contains(out, `"placed":false`) || !strings.Contains(out, "available") {
		t.Fatal(out)
	}
	for _, td := range anon {
		if td.InputSchema["additionalProperties"] != false || td.InputSchema["type"] != "object" {
			t.Fatalf("%s schema is not strict-friendly", td.Name)
		}
	}
}

func TestHistoryTurnsCollapsesAndDropsDangling(t *testing.T) {
	msgs := []StoredMessage{
		{Role: RoleAssistant, Text: "orphan"},
		{Role: RoleUser, Text: "a"},
		{Role: RoleUser, Text: "b"},
		{Role: RoleAssistant, Text: ""},
		{Role: RoleUser, Text: "dangling"},
	}
	turns := historyTurns(msgs)
	if len(turns) != 2 || turns[0].Text != "a\n\nb" || turns[1].Text != "(empty)" {
		t.Fatalf("%+v", turns)
	}
}
