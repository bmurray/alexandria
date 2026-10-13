package authn

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bmurray/alexandria/internal/auth"
	"github.com/bmurray/alexandria/internal/db"
	"github.com/bmurray/alexandria/internal/middleware"
)

// newTestServer wires the handler behind the session middleware, the way
// the real server does, against a fresh in-memory database. idpURL, when
// set, is the issuer of a "google" OIDC provider.
func newTestServer(t *testing.T, devLogin bool, idpURL string) (*httptest.Server, *auth.Service) {
	t.Helper()
	ctx := context.Background()
	database, err := db.OpenTest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := auth.Config{
		PublicURL:       "http://localhost:24380",
		DevLogin:        devLogin,
		SessionDuration: 2 * time.Hour,
		Admins:          []string{"root@example.com"},
	}
	if idpURL != "" {
		cfg.OIDC = []auth.OIDCProvider{{Name: "google", DisplayName: "Google", Issuer: idpURL, ClientID: "cid"}}
	}
	svc, err := auth.NewService(ctx, database, cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	New(svc, "Test Library", true, 21).Register(mux)
	srv := httptest.NewServer(middleware.Session(svc)(mux))
	t.Cleanup(srv.Close)
	return srv, svc
}

// client is a tiny JSON HTTP client that carries one cookie jar entry.
type client struct {
	t      *testing.T
	base   string
	cookie *http.Cookie
}

func (c *client) do(method, path string, body any) (*http.Response, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatal(err)
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, &buf)
	if err != nil {
		c.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, ck := range resp.Cookies() {
		if ck.Name == auth.DefaultCookieName {
			if ck.MaxAge < 0 {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	var out map[string]any
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		_ = json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp, out
}

func TestConfig(t *testing.T) {
	srv, _ := newTestServer(t, true, "https://accounts.google.com")
	c := &client{t: t, base: srv.URL}
	resp, body := c.do(http.MethodGet, "/api/config", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if body["libraryName"] != "Test Library" || body["aiEnabled"] != true || body["loanDays"] != float64(21) {
		t.Fatalf("config: %v", body)
	}
	providers, _ := body["providers"].([]any)
	if len(providers) != 2 {
		t.Fatalf("providers: %v", body["providers"])
	}
	first, _ := providers[0].(map[string]any)
	last, _ := providers[1].(map[string]any)
	if first["name"] != "google" || first["displayName"] != "Google" || first["type"] != "oidc" || last["name"] != "dev" || last["type"] != "dev" {
		t.Fatalf("providers: %v", providers)
	}
}

func TestDevLoginMeLogout(t *testing.T) {
	srv, svc := newTestServer(t, true, "")
	c := &client{t: t, base: srv.URL}

	resp, _ := c.do(http.MethodGet, "/api/auth/me", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous me: %d", resp.StatusCode)
	}

	resp, body := c.do(http.MethodPost, "/api/auth/dev/login", map[string]any{"email": "Ann@Example.com", "name": "Ann"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dev login: %d %v", resp.StatusCode, body)
	}
	user, _ := body["user"].(map[string]any)
	if user["email"] != "ann@example.com" || user["role"] != "member" || user["name"] != "Ann" {
		t.Fatalf("user: %v", user)
	}
	if c.cookie == nil {
		t.Fatal("no session cookie set")
	}
	if !c.cookie.HttpOnly || c.cookie.Path != "/" || c.cookie.SameSite != http.SameSiteLaxMode || c.cookie.Secure ||
		c.cookie.MaxAge != int(svc.SessionDuration().Seconds()) {
		t.Fatalf("cookie attributes: %+v", c.cookie)
	}

	resp, body = c.do(http.MethodGet, "/api/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: %d %v", resp.StatusCode, body)
	}
	if me, _ := body["user"].(map[string]any); me["id"] != user["id"] {
		t.Fatalf("me: %v", body)
	}

	// Validation errors come back as 400 JSON.
	resp, body = c.do(http.MethodPost, "/api/auth/dev/login", map[string]any{"email": "not-an-email"})
	if resp.StatusCode != http.StatusBadRequest || body["error"] != auth.ErrInvalidEmail.Error() {
		t.Fatalf("bad email: %d %v", resp.StatusCode, body)
	}
	resp, body = c.do(http.MethodPost, "/api/auth/dev/login", map[string]any{"email": "x@y.z", "role": "god"})
	if resp.StatusCode != http.StatusBadRequest || body["error"] != auth.ErrInvalidRole.Error() {
		t.Fatalf("bad role: %d %v", resp.StatusCode, body)
	}
	resp, body = c.do(http.MethodPost, "/api/auth/dev/login", map[string]any{"email": "x@y.z", "extra": 1})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field: %d %v", resp.StatusCode, body)
	}

	token := c.cookie.Value
	resp, _ = c.do(http.MethodPost, "/api/auth/logout", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if c.cookie != nil {
		t.Fatal("logout should clear the cookie")
	}
	if _, _, err := svc.Authenticate(context.Background(), token); err == nil {
		t.Fatal("session should be gone after logout")
	}
	// Logging out while signed out is still a 204.
	if resp, _ = c.do(http.MethodPost, "/api/auth/logout", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout again: %d", resp.StatusCode)
	}
}

func TestDevLoginDisabled(t *testing.T) {
	srv, _ := newTestServer(t, false, "")
	c := &client{t: t, base: srv.URL}
	resp, body := c.do(http.MethodPost, "/api/auth/dev/login", map[string]any{"email": "a@b.c"})
	if resp.StatusCode != http.StatusNotFound || body["error"] == "" {
		t.Fatalf("dev login disabled: %d %v", resp.StatusCode, body)
	}
	resp, body = c.do(http.MethodGet, "/api/config", nil)
	if ps, _ := body["providers"].([]any); resp.StatusCode != http.StatusOK || len(ps) != 0 {
		t.Fatalf("config with nothing enabled: %d %v", resp.StatusCode, body)
	}
}

func TestUsersRequireAdmin(t *testing.T) {
	srv, _ := newTestServer(t, true, "")
	anon := &client{t: t, base: srv.URL}
	if resp, _ := anon.do(http.MethodGet, "/api/users", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list: %d", resp.StatusCode)
	}

	member := &client{t: t, base: srv.URL}
	_, body := member.do(http.MethodPost, "/api/auth/dev/login", map[string]any{"email": "m@example.com"})
	memberID := body["user"].(map[string]any)["id"].(string)
	if resp, _ := member.do(http.MethodGet, "/api/users", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member list: %d", resp.StatusCode)
	}
	if resp, _ := member.do(http.MethodPatch, "/api/users/"+memberID, map[string]any{"role": "admin"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member self-promote: %d", resp.StatusCode)
	}

	admin := &client{t: t, base: srv.URL}
	_, body = admin.do(http.MethodPost, "/api/auth/dev/login", map[string]any{"email": "root@example.com", "name": "Root"})
	adminUser := body["user"].(map[string]any)
	if adminUser["role"] != "admin" {
		t.Fatalf("admin list should promote: %v", adminUser)
	}
	adminID := adminUser["id"].(string)

	resp, body := admin.do(http.MethodGet, "/api/users", nil)
	if users, _ := body["users"].([]any); resp.StatusCode != http.StatusOK || len(users) != 2 {
		t.Fatalf("admin list: %d %v", resp.StatusCode, body)
	}
	resp, body = admin.do(http.MethodPatch, "/api/users/"+memberID, map[string]any{"role": "librarian"})
	if resp.StatusCode != http.StatusOK || body["user"].(map[string]any)["role"] != "librarian" {
		t.Fatalf("set role: %d %v", resp.StatusCode, body)
	}
	// The promoted member sees the new role on their next request.
	if _, body = member.do(http.MethodGet, "/api/auth/me", nil); body["user"].(map[string]any)["role"] != "librarian" {
		t.Fatalf("role not live: %v", body)
	}

	cases := []struct {
		id   string
		role string
		want int
	}{
		{memberID, "god", http.StatusBadRequest},
		{"missing", "member", http.StatusNotFound},
		{adminID, "member", http.StatusConflict},
	}
	for _, tc := range cases {
		resp, body := admin.do(http.MethodPatch, "/api/users/"+tc.id, map[string]any{"role": tc.role})
		if resp.StatusCode != tc.want || body["error"] == "" {
			t.Errorf("PATCH %s role=%s: got %d %v, want %d", tc.id, tc.role, resp.StatusCode, body, tc.want)
		}
	}
}

func TestOIDCStartAndCallbackErrors(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "http://" + r.Host,
			"authorization_endpoint": "http://" + r.Host + "/authorize",
			"token_endpoint":         "http://" + r.Host + "/token",
			"jwks_uri":               "http://" + r.Host + "/jwks",
		})
	}))
	defer idp.Close()
	srv, _ := newTestServer(t, false, idp.URL)
	c := &client{t: t, base: srv.URL}

	resp, _ := c.do(http.MethodGet, "/api/auth/oidc/google/start?returnTo=%2Fbooks", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start: %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, idp.URL+"/authorize?") || !strings.Contains(loc, "state=") || !strings.Contains(loc, "client_id=cid") {
		t.Fatalf("start location: %s", loc)
	}

	resp, body := c.do(http.MethodGet, "/api/auth/oidc/github/start", nil)
	if resp.StatusCode != http.StatusNotFound || body["error"] != auth.ErrUnknownProvider.Error() {
		t.Fatalf("unknown provider: %d %v", resp.StatusCode, body)
	}
	resp, body = c.do(http.MethodGet, "/api/auth/oidc/google/start?returnTo=https%3A%2F%2Fevil.com", nil)
	if resp.StatusCode != http.StatusBadRequest || body["error"] != auth.ErrInvalidReturnTo.Error() {
		t.Fatalf("bad returnTo: %d %v", resp.StatusCode, body)
	}

	// A provider whose discovery document cannot be fetched is a 502.
	down, _ := newTestServer(t, false, "http://127.0.0.1:1")
	dc := &client{t: t, base: down.URL}
	resp, body = dc.do(http.MethodGet, "/api/auth/oidc/google/start", nil)
	if resp.StatusCode != http.StatusBadGateway || body["error"] != auth.ErrProviderUnavailable.Error() {
		t.Fatalf("discovery down: %d %v", resp.StatusCode, body)
	}

	// Callback errors redirect to the web app's callback route with ?error=.
	resp, _ = c.do(http.MethodGet, "/api/auth/oidc/google/callback?state=bogus&code=x", nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/auth/callback?error="+url.QueryEscape(auth.ErrChallengeExpired.Error()) {
		t.Fatalf("bad state callback: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = c.do(http.MethodGet, "/api/auth/oidc/google/callback?error=access_denied&error_description=User+cancelled", nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/auth/callback?error=User+cancelled" {
		t.Fatalf("idp error callback: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if c.cookie != nil {
		t.Fatal("no cookie may be set on a failed callback")
	}
}
