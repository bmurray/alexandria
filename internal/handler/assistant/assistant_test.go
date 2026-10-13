package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bmurray/alexandria/internal/ai"
	"github.com/bmurray/alexandria/internal/auth"
	"github.com/bmurray/alexandria/internal/handler"
	"github.com/bmurray/alexandria/internal/middleware"
)

// fakeService records the last call and answers from canned values.
type fakeService struct {
	owner, convID, message string
	caller                 *ai.Caller
	patched                struct {
		name     string
		enabled  *bool
		priority *int
		limit    *float64
		clear    bool
	}
	enrichUser string
	err        error
}

func (f *fakeService) Chat(_ context.Context, owner string, caller *ai.Caller, convID, message string) (*ai.ChatResult, error) {
	f.owner, f.caller, f.convID, f.message = owner, caller, convID, message
	if f.err != nil {
		return nil, f.err
	}
	return &ai.ChatResult{ConversationID: "c1", Reply: "hello " + message, ToolCalls: []ai.ToolCall{}, Usage: ai.ChatUsage{Provider: "opus"}}, nil
}
func (f *fakeService) ListConversations(_ context.Context, owner string) ([]ai.Conversation, error) {
	f.owner = owner
	return []ai.Conversation{{ID: "c1", Title: "t"}}, nil
}
func (f *fakeService) GetConversation(_ context.Context, owner, id string) (*ai.Conversation, []ai.StoredMessage, error) {
	f.owner = owner
	if id != "c1" {
		return nil, nil, handler.NewStatusError(404, "conversation not found")
	}
	return &ai.Conversation{ID: id}, []ai.StoredMessage{{Role: ai.RoleUser, Text: "hi"}}, nil
}
func (f *fakeService) DeleteConversation(_ context.Context, owner, id string) error {
	f.owner = owner
	return f.err
}
func (f *fakeService) Usage(context.Context) (*ai.UsageReport, error) {
	return &ai.UsageReport{Providers: []ai.ProviderState{{Name: "opus"}}, ByPurpose: []ai.PurposeTotals{}}, nil
}
func (f *fakeService) UpdateProvider(_ context.Context, name string, enabled *bool, priority *int, limit *float64, clear bool) (*ai.ProviderState, error) {
	f.patched.name, f.patched.enabled, f.patched.priority, f.patched.limit, f.patched.clear = name, enabled, priority, limit, clear
	return &ai.ProviderState{Name: name, Enabled: true}, nil
}
func (f *fakeService) Enrich(_ context.Context, in ai.EnrichInput, userID string) (*ai.EnrichResult, error) {
	f.enrichUser = userID
	g := "Fiction"
	return &ai.EnrichResult{Suggestion: ai.Suggestion{Genre: &g}, Source: "model"}, nil
}
func (f *fakeService) Lookup(_ context.Context, isbn, title, author string) (*ai.Suggestion, string, error) {
	a := "Someone"
	return &ai.Suggestion{Author: &a}, "openlibrary", nil
}

func newMux(f *fakeService) *http.ServeMux {
	mux := http.NewServeMux()
	New(f).Register(mux)
	return mux
}

func do(t *testing.T, mux *http.ServeMux, method, path, body string, u *auth.User, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	if u != nil {
		req = req.WithContext(middleware.WithUser(req.Context(), u))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestChatAnonymousSetsCookieAndOwner(t *testing.T) {
	f := &fakeService{}
	mux := newMux(f)
	rec := do(t, mux, "POST", "/api/ai/chat", `{"message":"  Do you have Dune? "}`, nil)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var res ai.ChatResult
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res.ConversationID != "c1" || res.Reply != "hello Do you have Dune?" || f.message != "Do you have Dune?" {
		t.Fatalf("%+v", res)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == AnonCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 30*24*3600 || cookie.Path != "/" {
		t.Fatalf("cookie: %+v", cookie)
	}
	if f.owner != "anon:"+cookie.Value || f.caller != nil {
		t.Fatalf("owner %q caller %v", f.owner, f.caller)
	}
	// The cookie is reused, not reissued.
	rec = do(t, mux, "POST", "/api/ai/chat", `{"conversationId":"c1","message":"again"}`, nil, cookie)
	if rec.Code != 200 || len(rec.Result().Cookies()) != 0 || f.owner != "anon:"+cookie.Value || f.convID != "c1" {
		t.Fatalf("%d cookies=%v owner=%s", rec.Code, rec.Result().Cookies(), f.owner)
	}
}

func TestChatSignedInAndValidation(t *testing.T) {
	f := &fakeService{}
	mux := newMux(f)
	u := &auth.User{ID: "u1", Name: "Ada", Role: auth.RoleMember}
	rec := do(t, mux, "POST", "/api/ai/chat", `{"message":"hi"}`, u)
	if rec.Code != 200 || f.owner != "u1" || f.caller == nil || f.caller.Name != "Ada" || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("%d owner=%s caller=%+v", rec.Code, f.owner, f.caller)
	}
	if rec := do(t, mux, "POST", "/api/ai/chat", `{"message":"   "}`, u); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	if rec := do(t, mux, "POST", "/api/ai/chat", `{"message":"`+strings.Repeat("x", 4001)+`"}`, u); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	if rec := do(t, mux, "POST", "/api/ai/chat", `{"bogus":1}`, u); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	f.err = ai.ErrNoProvider
	if rec := do(t, mux, "POST", "/api/ai/chat", `{"message":"hi"}`, u); rec.Code != 503 {
		t.Fatal(rec.Code)
	}
}

func TestConversations(t *testing.T) {
	f := &fakeService{}
	mux := newMux(f)
	// Nobody and no cookie: nothing is yours.
	rec := do(t, mux, "GET", "/api/ai/conversations", "", nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"conversations":[]}` {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, mux, "GET", "/api/ai/conversations/c1", "", nil); rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	if rec := do(t, mux, "DELETE", "/api/ai/conversations/c1", "", nil); rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	anon := &http.Cookie{Name: AnonCookie, Value: "tok"}
	rec = do(t, mux, "GET", "/api/ai/conversations", "", nil, anon)
	if rec.Code != 200 || f.owner != "anon:tok" || !strings.Contains(rec.Body.String(), `"id":"c1"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = do(t, mux, "GET", "/api/ai/conversations/c1", "", &auth.User{ID: "u1"})
	if rec.Code != 200 || f.owner != "u1" || !strings.Contains(rec.Body.String(), `"content":"hi"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, mux, "GET", "/api/ai/conversations/zz", "", &auth.User{ID: "u1"}); rec.Code != 404 {
		t.Fatal(rec.Code)
	}
	if rec := do(t, mux, "DELETE", "/api/ai/conversations/c1", "", &auth.User{ID: "u1"}); rec.Code != 204 {
		t.Fatal(rec.Code)
	}
}

func TestRoleGates(t *testing.T) {
	mux := newMux(&fakeService{})
	member := &auth.User{ID: "m", Role: auth.RoleMember}
	librarian := &auth.User{ID: "l", Role: auth.RoleLibrarian}
	admin := &auth.User{ID: "a", Role: auth.RoleAdmin}
	cases := []struct {
		method, path, body string
		user               *auth.User
		want               int
	}{
		{"GET", "/api/ai/usage", "", nil, 401},
		{"GET", "/api/ai/usage", "", librarian, 403},
		{"GET", "/api/ai/usage", "", admin, 200},
		{"PATCH", "/api/ai/providers/opus", `{"enabled":false}`, librarian, 403},
		{"PATCH", "/api/ai/providers/opus", `{"enabled":false}`, admin, 200},
		{"POST", "/api/ai/enrich", `{"title":"Dune"}`, member, 403},
		{"POST", "/api/ai/enrich", `{"title":"Dune"}`, librarian, 200},
		{"POST", "/api/ai/enrich", `{"title":""}`, librarian, 400},
		{"POST", "/api/books/lookup", `{"isbn":"1"}`, nil, 401},
		{"POST", "/api/books/lookup", `{"isbn":"1"}`, librarian, 200},
		{"POST", "/api/books/lookup", `{}`, librarian, 400},
	}
	for _, c := range cases {
		rec := do(t, mux, c.method, c.path, c.body, c.user)
		if rec.Code != c.want {
			t.Errorf("%s %s as %v: got %d want %d (%s)", c.method, c.path, c.user, rec.Code, c.want, rec.Body)
		}
	}
}

func TestUpdateProviderBody(t *testing.T) {
	f := &fakeService{}
	mux := newMux(f)
	admin := &auth.User{ID: "a", Role: auth.RoleAdmin}
	rec := do(t, mux, "PATCH", "/api/ai/providers/opus", `{"enabled":true,"priority":5,"spendLimitUsd":12.5}`, admin)
	if rec.Code != 200 || f.patched.name != "opus" || !*f.patched.enabled || *f.patched.priority != 5 || *f.patched.limit != 12.5 || f.patched.clear {
		t.Fatalf("%d %+v", rec.Code, f.patched)
	}
	if !strings.Contains(rec.Body.String(), `"provider":{"name":"opus"`) {
		t.Fatal(rec.Body.String())
	}
	rec = do(t, mux, "PATCH", "/api/ai/providers/opus", `{"spendLimitUsd":null}`, admin)
	if rec.Code != 200 || !f.patched.clear || f.patched.limit != nil || f.patched.enabled != nil {
		t.Fatalf("%d %+v", rec.Code, f.patched)
	}
	if rec := do(t, mux, "PATCH", "/api/ai/providers/opus", `{"spendLimitUsd":"lots"}`, admin); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	if rec := do(t, mux, "PATCH", "/api/ai/providers/opus", `{}`, admin); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	if rec := do(t, mux, "PATCH", "/api/ai/providers/opus", `nope`, admin); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
}

func TestEnrichAndLookupBodies(t *testing.T) {
	f := &fakeService{}
	mux := newMux(f)
	librarian := &auth.User{ID: "lib", Role: auth.RoleLibrarian}
	rec := do(t, mux, "POST", "/api/ai/enrich", `{"title":"Dune","author":"","isbn":"","description":""}`, librarian)
	if rec.Code != 200 || f.enrichUser != "lib" || !strings.Contains(rec.Body.String(), `"source":"model"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, mux, "POST", "/api/ai/enrich", `{"title":`, librarian); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
	rec = do(t, mux, "POST", "/api/books/lookup", `{"title":"Emma","author":"Austen"}`, librarian)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"source":"openlibrary"`) || !strings.Contains(rec.Body.String(), `"author":"Someone"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := do(t, mux, "POST", "/api/books/lookup", `{"isbn":`, librarian); rec.Code != 400 {
		t.Fatal(rec.Code)
	}
}
