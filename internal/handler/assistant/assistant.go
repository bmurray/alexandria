// Package assistant serves the AI endpoints: Ask the Librarian chat,
// conversation history, admin spend and provider controls, Enrich and the
// Open Library lookup.
package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bmurray/alexandria/internal/ai"
	"github.com/bmurray/alexandria/internal/auth"
	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/id"
	"github.com/bmurray/alexandria/internal/middleware"
)

// AnonCookie identifies an anonymous chat owner across requests.
const AnonCookie = "alexandria_anon"

const anonCookieMaxAge = 30 * 24 * time.Hour

// ChatService is what the handler needs from *ai.Service; tests fake it.
type ChatService interface {
	Chat(ctx context.Context, owner string, caller *ai.Caller, conversationID, message string) (*ai.ChatResult, error)
	ListConversations(ctx context.Context, owner string) ([]ai.Conversation, error)
	GetConversation(ctx context.Context, owner, id string) (*ai.Conversation, []ai.StoredMessage, error)
	DeleteConversation(ctx context.Context, owner, id string) error
	Usage(ctx context.Context) (*ai.UsageReport, error)
	UpdateProvider(ctx context.Context, name string, enabled *bool, priority *int, spendLimitUSD *float64, clearLimit bool) (*ai.ProviderState, error)
	Enrich(ctx context.Context, in ai.EnrichInput, userID string) (*ai.EnrichResult, error)
	Lookup(ctx context.Context, isbn, title, author string) (*ai.Suggestion, string, error)
}

var _ ChatService = (*ai.Service)(nil)

// Handler holds the routes.
type Handler struct {
	svc ChatService
}

// New builds the handler.
func New(svc ChatService) *Handler { return &Handler{svc: svc} }

// Register mounts the routes on mux (which already sits behind the session
// middleware).
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ai/chat", h.chat)
	mux.HandleFunc("GET /api/ai/conversations", h.listConversations)
	mux.HandleFunc("GET /api/ai/conversations/{id}", h.getConversation)
	mux.HandleFunc("DELETE /api/ai/conversations/{id}", h.deleteConversation)
	mux.HandleFunc("GET /api/ai/usage", middleware.RequireRole(auth.RoleAdmin, h.usage))
	mux.HandleFunc("PATCH /api/ai/providers/{name}", middleware.RequireRole(auth.RoleAdmin, h.updateProvider))
	mux.HandleFunc("POST /api/ai/enrich", middleware.RequireRole(auth.RoleLibrarian, h.enrich))
	mux.HandleFunc("POST /api/books/lookup", middleware.RequireRole(auth.RoleLibrarian, h.lookup))
}

// owner resolves who owns conversations for this request: the user id when
// signed in, else "anon:<cookie>". With create, a missing anonymous cookie
// is minted; otherwise "" means "no conversations can belong to you".
func owner(w http.ResponseWriter, r *http.Request, create bool) (string, *ai.Caller) {
	if u := middleware.UserFrom(r.Context()); u != nil {
		return u.ID, &ai.Caller{ID: u.ID, Name: u.Name}
	}
	if c, err := r.Cookie(AnonCookie); err == nil && c.Value != "" {
		return "anon:" + c.Value, nil
	}
	if !create {
		return "", nil
	}
	token := id.Token(24)
	http.SetCookie(w, &http.Cookie{
		Name:     AnonCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(anonCookieMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
	return "anon:" + token, nil
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ConversationID string `json:"conversationId"`
		Message        string `json:"message"`
	}
	if err := handler.Decode(r, &body); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" {
		handler.WriteProblems(w, []string{"message is required"})
		return
	}
	if utf8.RuneCountInString(body.Message) > ai.MessageMax {
		handler.WriteProblems(w, []string{"message must be at most 4000 characters"})
		return
	}
	own, caller := owner(w, r, true)
	res, err := h.svc.Chat(r.Context(), own, caller, body.ConversationID, body.Message)
	if err != nil {
		handler.WriteServiceError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) {
	own, _ := owner(w, r, false)
	list := []ai.Conversation{}
	if own != "" {
		var err error
		if list, err = h.svc.ListConversations(r.Context(), own); err != nil {
			handler.WriteServiceError(w, r, err)
			return
		}
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request) {
	own, _ := owner(w, r, false)
	if own == "" {
		handler.WriteError(w, http.StatusNotFound, "conversation not found")
		return
	}
	conv, msgs, err := h.svc.GetConversation(r.Context(), own, r.PathValue("id"))
	if err != nil {
		handler.WriteServiceError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"conversation": conv, "messages": msgs})
}

func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) {
	own, _ := owner(w, r, false)
	if own == "" {
		handler.WriteError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if err := h.svc.DeleteConversation(r.Context(), own, r.PathValue("id")); err != nil {
		handler.WriteServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) usage(w http.ResponseWriter, r *http.Request) {
	report, err := h.svc.Usage(r.Context())
	if err != nil {
		handler.WriteServiceError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) updateProvider(w http.ResponseWriter, r *http.Request) {
	// spendLimitUsd distinguishes absent (keep), null (clear) and a number.
	var body struct {
		Enabled       *bool           `json:"enabled"`
		Priority      *int            `json:"priority"`
		SpendLimitUSD json.RawMessage `json:"spendLimitUsd"`
	}
	if err := handler.Decode(r, &body); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	var limit *float64
	clear := false
	switch raw := strings.TrimSpace(string(body.SpendLimitUSD)); {
	case raw == "":
	case raw == "null":
		clear = true
	default:
		var v float64
		if err := json.Unmarshal(body.SpendLimitUSD, &v); err != nil {
			handler.WriteProblems(w, []string{"spendLimitUsd must be a number or null"})
			return
		}
		limit = &v
	}
	if body.Enabled == nil && body.Priority == nil && limit == nil && !clear {
		handler.WriteProblems(w, []string{"nothing to update"})
		return
	}
	state, err := h.svc.UpdateProvider(r.Context(), r.PathValue("name"), body.Enabled, body.Priority, limit, clear)
	if err != nil {
		handler.WriteServiceError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"provider": state})
}

func (h *Handler) enrich(w http.ResponseWriter, r *http.Request) {
	var in ai.EnrichInput
	if err := handler.Decode(r, &in); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.ISBN) == "" {
		handler.WriteProblems(w, []string{"title or isbn is required"})
		return
	}
	res, err := h.svc.Enrich(r.Context(), in, middleware.UserFrom(r.Context()).ID)
	if err != nil {
		handler.WriteServiceError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ISBN   string `json:"isbn"`
		Title  string `json:"title"`
		Author string `json:"author"`
	}
	if err := handler.Decode(r, &body); err != nil {
		handler.WriteError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(body.ISBN) == "" && strings.TrimSpace(body.Title) == "" {
		handler.WriteProblems(w, []string{"isbn or title is required"})
		return
	}
	sug, source, err := h.svc.Lookup(r.Context(), body.ISBN, body.Title, body.Author)
	if err != nil {
		handler.WriteServiceError(w, r, err)
		return
	}
	handler.WriteJSON(w, http.StatusOK, map[string]any{"suggestion": sug, "source": source})
}
