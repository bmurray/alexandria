package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/id"
)

// Conversation is one chat thread. Owner is a user id or "anon:<token>".
type Conversation struct {
	ID        string    `json:"id"`
	Owner     string    `json:"-"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// StoredMessage is one persisted turn as returned by GetConversation.
type StoredMessage struct {
	ID        string     `json:"id"`
	Role      Role       `json:"role"`
	Text      string     `json:"content"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// errConversationNotFound is a 404 for a missing or foreign conversation; it
// never reveals which.
var errConversationNotFound = handler.NewStatusError(http.StatusNotFound, "conversation not found")

// store persists conversations and messages. The message content column
// holds a JSON Message so tool calls travel with the assistant turn.
type store struct {
	db  *sql.DB
	now func() time.Time
}

const titleMax = 60

func (s *store) createConversation(ctx context.Context, owner, firstMessage string) (*Conversation, error) {
	now := s.now().UTC()
	c := &Conversation{ID: id.New(), Owner: owner, Title: truncate(firstMessage, titleMax), CreatedAt: now, UpdatedAt: now}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ai_conversations (id, owner, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID, c.Owner, c.Title, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("create conversation: %w", err)
	}
	return c, nil
}

// getConversation returns the conversation when owner matches, else 404.
func (s *store) getConversation(ctx context.Context, owner, convID string) (*Conversation, error) {
	var c Conversation
	var created, updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner, title, created_at, updated_at FROM ai_conversations WHERE id = ? AND owner = ?`, convID, owner).
		Scan(&c.ID, &c.Owner, &c.Title, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	c.CreatedAt, _ = time.Parse(time.RFC3339, created)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
	return &c, nil
}

func (s *store) listConversations(ctx context.Context, owner string) ([]Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner, title, created_at, updated_at FROM ai_conversations WHERE owner = ? ORDER BY updated_at DESC, id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		var created, updated string
		if err := rows.Scan(&c.ID, &c.Owner, &c.Title, &created, &updated); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, created)
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *store) deleteConversation(ctx context.Context, owner, convID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ai_conversations WHERE id = ? AND owner = ?`, convID, owner)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errConversationNotFound
	}
	return nil
}

func (s *store) touch(ctx context.Context, convID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ai_conversations SET updated_at = ? WHERE id = ?`, s.now().UTC().Format(time.RFC3339), convID)
	return err
}

func (s *store) addMessage(ctx context.Context, convID string, m Message) (*StoredMessage, error) {
	content, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	sm := &StoredMessage{ID: id.New(), Role: m.Role, Text: m.Text, ToolCalls: m.ToolCalls, CreatedAt: now}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ai_messages (id, conversation_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
		sm.ID, convID, string(m.Role), string(content), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("add message: %w", err)
	}
	return sm, nil
}

// messages returns the conversation's turns oldest first. limit > 0 keeps
// only the most recent limit turns.
func (s *store) messages(ctx context.Context, convID string, limit int) ([]StoredMessage, error) {
	q := `SELECT id, role, content, created_at FROM ai_messages WHERE conversation_id = ? ORDER BY created_at, rowid`
	args := []any{convID}
	if limit > 0 {
		// Newest N, then flip to chronological below.
		q = `SELECT id, role, content, created_at FROM ai_messages WHERE conversation_id = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoredMessage{}
	for rows.Next() {
		var sm StoredMessage
		var role, content, created string
		if err := rows.Scan(&sm.ID, &role, &content, &created); err != nil {
			return nil, err
		}
		var m Message
		if err := json.Unmarshal([]byte(content), &m); err != nil {
			m = Message{Text: content} // tolerate a plain-text row
		}
		sm.Role, sm.Text, sm.ToolCalls = Role(role), m.Text, m.ToolCalls
		sm.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, sm)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if limit > 0 {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
