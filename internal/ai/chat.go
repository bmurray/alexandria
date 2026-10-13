package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/bmurray/alexandria/internal/handler"
)

// MessageMax is the longest chat message accepted.
const MessageMax = 4000

const defaultSystemPrompt = `You are the librarian of Alexandria, a small community library. Be warm, concise and concrete.
Use the tools to look things up in this library's catalog before answering; never invent titles or availability.
When recommending, suggest books that are actually in the catalog and say in one line why each fits.
If a book has no copies available, mention holds. Keep answers short; use plain prose, not headings.`

const refusalReply = "I can't help with that one."

// ChatResult is the reply to one chat turn.
type ChatResult struct {
	ConversationID string     `json:"conversationId"`
	Reply          string     `json:"reply"`
	ToolCalls      []ToolCall `json:"toolCalls"`
	Usage          ChatUsage  `json:"usage"`
}

// ChatUsage sums every model round of the turn. Provider and Model are from
// the round that produced the final reply.
type ChatUsage struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
}

// Chat runs one turn of Ask the Librarian. owner keys the conversation (a
// user id or "anon:<token>"); caller is nil when nobody is signed in.
// conversationID may be empty to start a new thread.
func (s *Service) Chat(ctx context.Context, owner string, caller *Caller, conversationID, message string) (*ChatResult, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, handler.NewStatusError(http.StatusBadRequest, "message is required")
	}
	if utf8.RuneCountInString(message) > MessageMax {
		return nil, handler.NewStatusError(http.StatusBadRequest, fmt.Sprintf("message is longer than %d characters", MessageMax))
	}
	if ok, err := s.hasEligibleProvider(ctx); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNoProvider
	}
	if err := s.checkDailyBudget(ctx); err != nil {
		return nil, err
	}

	var conv *Conversation
	var err error
	if conversationID == "" {
		conv, err = s.store.createConversation(ctx, owner, message)
	} else {
		conv, err = s.store.getConversation(ctx, owner, conversationID)
	}
	if err != nil {
		return nil, err
	}
	history, err := s.store.messages(ctx, conv.ID, s.cfg.HistoryTurns)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.addMessage(ctx, conv.ID, Message{Role: RoleUser, Text: message}); err != nil {
		return nil, err
	}

	userID := ""
	if caller != nil {
		userID = caller.ID
	}
	req := Request{
		System:   s.systemPrompt(caller),
		Tools:    toolset(s.catalog, caller),
		Messages: historyTurns(history),
	}
	req.Messages = append(req.Messages, Turn{Role: RoleUser, Text: message})

	reply, calls, usage, err := s.runLoop(ctx, userID, req)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.addMessage(ctx, conv.ID, Message{Role: RoleAssistant, Text: reply, ToolCalls: calls}); err != nil {
		return nil, err
	}
	if err := s.store.touch(ctx, conv.ID); err != nil {
		return nil, err
	}
	if calls == nil {
		calls = []ToolCall{}
	}
	return &ChatResult{ConversationID: conv.ID, Reply: reply, ToolCalls: calls, Usage: usage}, nil
}

// runLoop is the tool loop: ask the model, run every tool it requested, hand
// all results back in one user turn, repeat. The last allowed round goes out
// without tools so the model must answer with what it has.
func (s *Service) runLoop(ctx context.Context, userID string, req Request) (reply string, calls []ToolCall, usage ChatUsage, err error) {
	tools := req.Tools
	for round := 0; round <= s.cfg.MaxToolRounds; round++ {
		req.Tools = tools
		if round == s.cfg.MaxToolRounds {
			req.Tools = nil // out of tool rounds: answer with what you have
		}
		resp, p, err := s.complete(ctx, PurposeChat, userID, req)
		if err != nil {
			return "", nil, usage, err
		}
		usage.InputTokens += resp.Usage.InputTokens
		usage.OutputTokens += resp.Usage.OutputTokens
		usage.CostUSD = RoundMicro(usage.CostUSD + s.pricing[p.Name()].Cost(resp.Usage))
		usage.Provider, usage.Model = p.Name(), p.Model()

		switch {
		case resp.StopReason == StopRefusal:
			return refusalReply, calls, usage, nil
		case len(resp.ToolUses) == 0 || resp.StopReason == StopMaxTokens:
			text := resp.Text
			if text == "" {
				text = "I'm sorry, I lost my train of thought. Could you ask that again?"
			}
			return text, calls, usage, nil
		}

		results := make([]ToolResult, 0, len(resp.ToolUses))
		for _, tu := range resp.ToolUses {
			out, isErr := s.runTool(ctx, tools, tu)
			results = append(results, ToolResult{ID: tu.ID, Output: out, IsError: isErr})
			calls = append(calls, ToolCall{Name: tu.Name, Input: validJSON(tu.Input), Output: out})
		}
		req.Messages = append(req.Messages,
			Turn{Role: RoleAssistant, Text: resp.Text, ToolUses: resp.ToolUses, Native: resp.Native},
			Turn{Role: RoleUser, ToolResults: results},
		)
	}
	return "", calls, usage, errors.New("ai: tool loop ended without a reply")
}

// runTool executes one requested tool. Unknown tools and failures become
// error results for the model rather than failing the turn.
func (s *Service) runTool(ctx context.Context, tools []ToolDef, tu ToolUse) (string, bool) {
	for _, t := range tools {
		if t.Name != tu.Name {
			continue
		}
		out, err := t.Run(ctx, tu.Input)
		if err != nil {
			slog.WarnContext(ctx, "ai tool failed", slog.String("tool", tu.Name), slog.String("error", err.Error()))
			return "tool error: " + err.Error(), true
		}
		return out, false
	}
	return "unknown tool " + tu.Name, true
}

// validJSON keeps a tool input persistable: anything that is not valid JSON
// (a truncated tool_use) is stored as a JSON string instead.
func validJSON(raw json.RawMessage) json.RawMessage {
	if json.Valid(raw) {
		return raw
	}
	quoted, _ := json.Marshal(string(raw))
	return quoted
}

func (s *Service) systemPrompt(caller *Caller) string {
	var b strings.Builder
	b.WriteString(s.cfg.SystemPrompt)
	b.WriteString("\n\nToday is ")
	b.WriteString(s.now().UTC().Format("Monday, 2 January 2006"))
	b.WriteString(". ")
	if caller != nil {
		name := caller.Name
		if name == "" {
			name = "a member"
		}
		b.WriteString("The person is signed in as " + name + "; my_loans and place_hold act for them.")
	} else {
		b.WriteString("The person is not signed in, so my_loans and place_hold are unavailable; suggest signing in if they ask for those.")
	}
	return b.String()
}

// historyTurns replays stored messages as plain text turns. Earlier tool
// calls are summarised into the assistant text so the model keeps context
// without re-sending tool blocks (which the API would reject without their
// matching results).
func historyTurns(msgs []StoredMessage) []Turn {
	turns := make([]Turn, 0, len(msgs))
	for _, m := range msgs {
		t := Turn{Role: m.Role, Text: m.Text}
		if t.Text == "" {
			t.Text = "(empty)"
		}
		turns = append(turns, t)
	}
	// The API wants alternating roles starting with user; collapse any
	// repeats defensively.
	out := turns[:0]
	for _, t := range turns {
		if len(out) == 0 && t.Role != RoleUser {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == t.Role {
			out[len(out)-1].Text += "\n\n" + t.Text
			continue
		}
		out = append(out, t)
	}
	if len(out) > 0 && out[len(out)-1].Role == RoleUser {
		// A dangling user turn (previous call failed mid-way) would collide
		// with the new message; fold it in.
		out = out[:len(out)-1]
	}
	return out
}

// ListConversations returns the owner's threads, most recent first.
func (s *Service) ListConversations(ctx context.Context, owner string) ([]Conversation, error) {
	return s.store.listConversations(ctx, owner)
}

// GetConversation returns a thread and its messages; 404 unless owner matches.
func (s *Service) GetConversation(ctx context.Context, owner, id string) (*Conversation, []StoredMessage, error) {
	conv, err := s.store.getConversation(ctx, owner, id)
	if err != nil {
		return nil, nil, err
	}
	msgs, err := s.store.messages(ctx, id, 0)
	if err != nil {
		return nil, nil, err
	}
	return conv, msgs, nil
}

// DeleteConversation removes a thread and its messages; 404 unless owner matches.
func (s *Service) DeleteConversation(ctx context.Context, owner, id string) error {
	return s.store.deleteConversation(ctx, owner, id)
}
