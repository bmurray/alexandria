package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bmurray/alexandria/internal/db"
	"github.com/bmurray/alexandria/internal/id"
	"github.com/bmurray/alexandria/internal/oidc"
)

// newTestService opens an in-memory database and a service with dev login
// on, a fixed clock and one OIDC provider named "google" whose client the
// tests may replace with a fake.
func newTestService(t *testing.T, mutate func(*Config)) (*Service, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	database, err := db.OpenTest(ctx)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := Config{
		PublicURL:       "http://localhost:24380/",
		DevLogin:        true,
		SessionDuration: time.Hour,
		Admins:          []string{"Root@Example.com"},
		OIDC: []OIDCProvider{{
			Name: "google", DisplayName: "Google", Issuer: "https://accounts.google.com", ClientID: "cid", ClientSecret: "sec",
		}},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	svc, err := NewService(ctx, database, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	base := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }
	return svc, database
}

// advance moves the service clock forward by d.
func advance(s *Service, d time.Duration) {
	now := s.now().Add(d)
	s.now = func() time.Time { return now }
}

// fakeIdP stands in for an identity provider: AuthURL needs no discovery
// and Exchange returns a canned identity.
type fakeIdP struct {
	ident        *oidc.Identity
	err          error
	gotCode      string
	gotVerifier  string
	gotNonce     string
	gotChallenge string
}

func (f *fakeIdP) AuthURL(_ context.Context, state, nonce, challenge string) (string, error) {
	f.gotChallenge = challenge
	return "https://idp.example/authorize?state=" + state + "&nonce=" + nonce + "&code_challenge=" + challenge, nil
}

func (f *fakeIdP) Exchange(_ context.Context, code, verifier, nonce string) (*oidc.Identity, error) {
	f.gotCode, f.gotVerifier, f.gotNonce = code, verifier, nonce
	if f.err != nil {
		return nil, f.err
	}
	return f.ident, nil
}

func TestNewServiceDefaultsAndValidation(t *testing.T) {
	svc, _ := newTestService(t, func(c *Config) { c.SessionDuration = 0; c.CookieName = "" })
	if svc.CookieName() != DefaultCookieName || svc.SessionDuration() != DefaultSessionDuration {
		t.Fatalf("defaults not applied: %q %v", svc.CookieName(), svc.SessionDuration())
	}
	if svc.PublicURL() != "http://localhost:24380" {
		t.Fatalf("PublicURL should drop the trailing slash: %q", svc.PublicURL())
	}
	if svc.redirectURL("google") != "http://localhost:24380/api/auth/oidc/google/callback" {
		t.Fatalf("redirect url: %q", svc.redirectURL("google"))
	}
	got := svc.Providers()
	if len(got) != 2 || got[0] != (ProviderInfo{Name: "google", DisplayName: "Google", Type: "oidc"}) ||
		got[1] != (ProviderInfo{Name: "dev", DisplayName: DefaultDevDisplayName, Type: "dev"}) {
		t.Fatalf("providers: %+v", got)
	}

	database, _ := db.OpenTest(context.Background())
	defer database.Close()
	bad := []Config{
		{OIDC: []OIDCProvider{{Name: "x", Issuer: "https://i", ClientID: "c"}}},                          // no PublicURL
		{PublicURL: "http://h", OIDC: []OIDCProvider{{Name: "dev", Issuer: "https://i", ClientID: "c"}}}, // reserved
		{PublicURL: "http://h", OIDC: []OIDCProvider{{Name: "x", ClientID: "c"}}},                        // no issuer
		{PublicURL: "http://h", OIDC: []OIDCProvider{{Name: "a/b", Issuer: "https://i", ClientID: "c"}}}, // bad name
		{PublicURL: "http://h", OIDC: []OIDCProvider{
			{Name: "x", Issuer: "https://i", ClientID: "c"}, {Name: "x", Issuer: "https://j", ClientID: "d"}}}, // dup
	}
	for i, c := range bad {
		if _, err := NewService(context.Background(), database, c); err == nil {
			t.Errorf("config %d should be rejected", i)
		}
	}
}

func TestSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)
	u, err := svc.DevLogin(ctx, "ann@example.com", "Ann", "")
	if err != nil {
		t.Fatal(err)
	}

	token, sess, err := svc.CreateSession(ctx, u, "UA/1", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || sess.ID != hashToken(token) || sess.UserID != u.ID {
		t.Fatalf("session: token=%q sess=%+v", token, sess)
	}
	if sess.ExpiresAt.Sub(sess.CreatedAt) != time.Hour {
		t.Fatalf("expiry: %v", sess.ExpiresAt.Sub(sess.CreatedAt))
	}

	gotUser, gotSess, err := svc.Authenticate(ctx, token)
	if err != nil || gotUser.ID != u.ID || gotSess.ID != sess.ID {
		t.Fatalf("authenticate: %v %+v %+v", err, gotUser, gotSess)
	}
	if !gotSess.LastSeenAt.Equal(sess.LastSeenAt) {
		t.Fatal("last_seen_at should not be touched within five minutes")
	}
	for _, bad := range []string{"", "nope", token + "x"} {
		if _, _, err := svc.Authenticate(ctx, bad); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("token %q: want ErrUnauthenticated, got %v", bad, err)
		}
	}

	// After five minutes the session is touched; after an hour it is expired.
	advance(svc, 6*time.Minute)
	_, gotSess, err = svc.Authenticate(ctx, token)
	if err != nil || !gotSess.LastSeenAt.Equal(svc.now()) {
		t.Fatalf("touch: err=%v last_seen=%v now=%v", err, gotSess.LastSeenAt, svc.now())
	}
	advance(svc, time.Hour)
	if _, _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session: want ErrUnauthenticated, got %v", err)
	}

	// Logout removes exactly that session; LogoutAll removes every one.
	advance(svc, -time.Hour)
	t1, _, _ := svc.CreateSession(ctx, u, "", "")
	t2, _, _ := svc.CreateSession(ctx, u, "", "")
	if err := svc.Logout(ctx, t1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Authenticate(ctx, t1); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("t1 should be gone")
	}
	if _, _, err := svc.Authenticate(ctx, t2); err != nil {
		t.Fatal("t2 should survive")
	}
	if err := svc.LogoutAll(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Authenticate(ctx, t2); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("t2 should be gone after LogoutAll")
	}
	if err := svc.Logout(ctx, "unknown"); err != nil {
		t.Fatalf("logging out an unknown token is not an error: %v", err)
	}
}

func TestDevLogin(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)

	u, err := svc.DevLogin(ctx, "  Bob@Example.COM ", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "bob@example.com" || u.Name != "bob" || u.Role != RoleMember || u.ID == "" {
		t.Fatalf("new user: %+v", u)
	}
	if !u.LastLoginAt.Equal(svc.now()) || !u.CreatedAt.Equal(svc.now()) {
		t.Fatalf("timestamps: %+v", u)
	}

	// Signing in again keeps the same account, refreshes name and last login.
	advance(svc, time.Minute)
	again, err := svc.DevLogin(ctx, "bob@example.com", "Robert", "")
	if err != nil || again.ID != u.ID || again.Name != "Robert" || !again.LastLoginAt.Equal(svc.now()) {
		t.Fatalf("repeat login: %v %+v", err, again)
	}
	if again.Role != RoleMember {
		t.Fatalf("role should be kept: %+v", again)
	}

	// An explicit role is applied, but the admin list wins.
	lib, err := svc.DevLogin(ctx, "bob@example.com", "", RoleLibrarian)
	if err != nil || lib.Role != RoleLibrarian {
		t.Fatalf("explicit role: %v %+v", err, lib)
	}
	root, err := svc.DevLogin(ctx, "root@example.com", "Root", RoleMember)
	if err != nil || root.Role != RoleAdmin {
		t.Fatalf("admin list should win: %v %+v", err, root)
	}
	// A user who is later added to the admin list is promoted on sign-in.
	svc.admins["bob@example.com"] = true
	promoted, err := svc.DevLogin(ctx, "bob@example.com", "", "")
	if err != nil || promoted.Role != RoleAdmin {
		t.Fatalf("promotion: %v %+v", err, promoted)
	}

	if _, err := svc.DevLogin(ctx, "bob@example.com", "", Role("owner")); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("want ErrInvalidRole, got %v", err)
	}
	for _, bad := range []string{"", "nope", "@x", "x@", "a@b@c", "a b@c"} {
		if _, err := svc.DevLogin(ctx, bad, "", ""); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("email %q: want ErrInvalidEmail, got %v", bad, err)
		}
	}

	off, _ := newTestService(t, func(c *Config) { c.DevLogin = false })
	if _, err := off.DevLogin(ctx, "x@y.z", "", ""); !errors.Is(err, ErrDevLoginDisabled) {
		t.Fatalf("want ErrDevLoginDisabled, got %v", err)
	}
	if ps := off.Providers(); len(ps) != 1 || ps[0].Name != "google" {
		t.Fatalf("dev must not be listed when off: %+v", ps)
	}
}

func TestAllowedEmailDomains(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, func(c *Config) { c.AllowedEmailDomains = []string{"@Example.com", "corp.io"} })
	for _, ok := range []string{"a@example.com", "b@CORP.IO"} {
		if _, err := svc.DevLogin(ctx, ok, "", ""); err != nil {
			t.Errorf("%s should be allowed: %v", ok, err)
		}
	}
	for _, bad := range []string{"a@sub.example.com", "b@gmail.com", "root@evil.com"} {
		if _, err := svc.DevLogin(ctx, bad, "", ""); !errors.Is(err, ErrEmailNotAllowed) {
			t.Errorf("%s: want ErrEmailNotAllowed, got %v", bad, err)
		}
	}
}

func TestSetRole(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, nil)
	admin, _ := svc.DevLogin(ctx, "root@example.com", "", "")
	member, _ := svc.DevLogin(ctx, "m@example.com", "", "")

	if _, err := svc.SetRole(ctx, member, admin.ID, RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member actor: want ErrForbidden, got %v", err)
	}
	if _, err := svc.SetRole(ctx, nil, admin.ID, RoleMember); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("nil actor: want ErrUnauthenticated, got %v", err)
	}
	if _, err := svc.SetRole(ctx, admin, member.ID, Role("god")); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("want ErrInvalidRole, got %v", err)
	}
	if _, err := svc.SetRole(ctx, admin, admin.ID, RoleLibrarian); !errors.Is(err, ErrSelfDemotion) {
		t.Fatalf("want ErrSelfDemotion, got %v", err)
	}
	if _, err := svc.SetRole(ctx, admin, admin.ID, RoleAdmin); err != nil {
		t.Fatalf("admin -> admin on self is a no-op, not an error: %v", err)
	}
	if _, err := svc.SetRole(ctx, admin, "missing", RoleMember); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	got, err := svc.SetRole(ctx, admin, member.ID, RoleLibrarian)
	if err != nil || got.Role != RoleLibrarian || got.ID != member.ID {
		t.Fatalf("set role: %v %+v", err, got)
	}
	if reread, _ := svc.GetUser(ctx, member.ID); reread.Role != RoleLibrarian {
		t.Fatalf("role not persisted: %+v", reread)
	}

	users, err := svc.ListUsers(ctx)
	if err != nil || len(users) != 2 || users[0].ID != admin.ID || users[1].ID != member.ID {
		t.Fatalf("list users: %v %+v", err, users)
	}
	if _, err := svc.GetUser(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetUser unknown: %v", err)
	}
}

// TestOIDCStartWithDiscovery uses the real oidc client against a local
// discovery document, so the whole start leg is exercised end to end.
func TestOIDCStartWithDiscovery(t *testing.T) {
	ctx := context.Background()
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "http://" + r.Host,
			"authorization_endpoint": "http://" + r.Host + "/authorize",
			"token_endpoint":         "http://" + r.Host + "/token",
			"jwks_uri":               "http://" + r.Host + "/jwks",
		})
	}))
	defer idp.Close()
	svc, database := newTestService(t, func(c *Config) { c.OIDC[0].Issuer = idp.URL })

	u, err := svc.OIDCStart(ctx, "google", "/books/42")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(u, idp.URL+"/authorize?") || !strings.Contains(u, "code_challenge_method=S256") ||
		!strings.Contains(u, "redirect_uri=http%3A%2F%2Flocalhost%3A24380%2Fapi%2Fauth%2Foidc%2Fgoogle%2Fcallback") {
		t.Fatalf("auth url: %s", u)
	}
	// The state in the URL is the id of the challenge row.
	var state string
	for _, part := range strings.Split(strings.SplitN(u, "?", 2)[1], "&") {
		if v, ok := strings.CutPrefix(part, "state="); ok {
			state = v
		}
	}
	var kind, payload string
	if err := database.QueryRowContext(ctx, "SELECT kind, payload FROM challenges WHERE id = ?", state).Scan(&kind, &payload); err != nil {
		t.Fatalf("challenge row for state %q: %v", state, err)
	}
	var ch oidcChallenge
	if err := json.Unmarshal([]byte(payload), &ch); err != nil || kind != "oidc" || ch.Provider != "google" || ch.ReturnTo != "/books/42" || ch.Verifier == "" || ch.Nonce == "" {
		t.Fatalf("challenge: kind=%s %+v err=%v", kind, ch, err)
	}

	if _, err := svc.OIDCStart(ctx, "github", "/"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("want ErrUnknownProvider, got %v", err)
	}
	for _, bad := range []string{"//evil.com", "/\\evil.com", "https://evil.com", "books", "/x\r\nSet-Cookie: a=b"} {
		if _, err := svc.OIDCStart(ctx, "google", bad); !errors.Is(err, ErrInvalidReturnTo) {
			t.Errorf("returnTo %q: want ErrInvalidReturnTo, got %v", bad, err)
		}
	}
	if _, err := svc.OIDCStart(ctx, "dev", "/"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("dev is not an OIDC provider: %v", err)
	}
}

func TestOIDCCallback(t *testing.T) {
	ctx := context.Background()
	svc, database := newTestService(t, nil)
	idp := &fakeIdP{ident: &oidc.Identity{Subject: "sub-1", Email: "Carol@Example.com", Name: "Carol", Picture: "https://img/c.png", EmailVerified: true}}
	svc.providers["google"] = idp

	if _, _, err := svc.OIDCCallback(ctx, "google", "bogus", "code"); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("bad state: want ErrChallengeExpired, got %v", err)
	}
	if _, _, err := svc.OIDCCallback(ctx, "google", "", ""); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("empty state/code: want ErrChallengeExpired, got %v", err)
	}
	if _, _, err := svc.OIDCCallback(ctx, "nope", "s", "c"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("want ErrUnknownProvider, got %v", err)
	}

	startURL, err := svc.OIDCStart(ctx, "google", "/account")
	if err != nil {
		t.Fatal(err)
	}
	state := strings.SplitN(strings.SplitN(startURL, "state=", 2)[1], "&", 2)[0]

	u, returnTo, err := svc.OIDCCallback(ctx, "google", state, "the-code")
	if err != nil {
		t.Fatal(err)
	}
	if returnTo != "/account" || u.Email != "carol@example.com" || u.Name != "Carol" || u.Picture != "https://img/c.png" || u.Role != RoleMember {
		t.Fatalf("callback user: %+v returnTo=%q", u, returnTo)
	}
	if idp.gotCode != "the-code" || idp.gotVerifier == "" || idp.gotNonce == "" {
		t.Fatalf("exchange inputs: %+v", idp)
	}
	// The verifier handed to Exchange matches the S256 challenge sent at start.
	if pkceChallenge(idp.gotVerifier) != idp.gotChallenge {
		t.Fatalf("PKCE verifier/challenge mismatch")
	}
	var n int
	_ = database.QueryRowContext(ctx, "SELECT count(*) FROM identities WHERE provider = 'google' AND subject = 'sub-1' AND user_id = ?", u.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("identity rows: %d", n)
	}

	// The state is single-use.
	if _, _, err := svc.OIDCCallback(ctx, "google", state, "the-code"); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("replayed state: want ErrChallengeExpired, got %v", err)
	}

	// An expired challenge is refused even though the row exists.
	startURL, _ = svc.OIDCStart(ctx, "google", "/")
	state = strings.SplitN(strings.SplitN(startURL, "state=", 2)[1], "&", 2)[0]
	advance(svc, challengeTTL+time.Second)
	if _, _, err := svc.OIDCCallback(ctx, "google", state, "c"); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("expired challenge: want ErrChallengeExpired, got %v", err)
	}

	// A failed exchange surfaces as ErrExchangeFailed.
	startURL, _ = svc.OIDCStart(ctx, "google", "/")
	state = strings.SplitN(strings.SplitN(startURL, "state=", 2)[1], "&", 2)[0]
	idp.err = oidc.ErrInvalidIDToken
	if _, _, err := svc.OIDCCallback(ctx, "google", state, "c"); !errors.Is(err, ErrExchangeFailed) || !errors.Is(err, oidc.ErrInvalidIDToken) {
		t.Fatalf("exchange failure: %v", err)
	}
	idp.err = nil

	// Second sign-in by the same subject: same user, admin promotion applies.
	svc.admins["carol@example.com"] = true
	startURL, _ = svc.OIDCStart(ctx, "google", "/")
	state = strings.SplitN(strings.SplitN(startURL, "state=", 2)[1], "&", 2)[0]
	again, _, err := svc.OIDCCallback(ctx, "google", state, "c")
	if err != nil || again.ID != u.ID || again.Role != RoleAdmin {
		t.Fatalf("second sign-in: %v %+v", err, again)
	}
}

// pkceChallenge recomputes the S256 code challenge for a verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// TestLinkIdentityByEmail covers the "existing account, new provider" path:
// a user who signed in with Dev and now arrives through Google with the same
// email must get the same account, with the new identity linked to it.
func TestLinkIdentityByEmail(t *testing.T) {
	ctx := context.Background()
	svc, database := newTestService(t, nil)
	now := svc.now()
	existing := &User{ID: id.New(), Email: "dave@example.com", Name: "Dave", Role: RoleLibrarian, CreatedAt: now, LastLoginAt: now}
	if err := svc.store.insertUser(ctx, database, existing); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.insertIdentity(ctx, database, id.New(), existing.ID, "dev", "dave@example.com", now); err != nil {
		t.Fatal(err)
	}

	advance(svc, time.Minute)
	u, err := svc.linkIdentity(ctx, "google", "g-sub", "dave@example.com", "", "https://img/d.png", "")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != existing.ID || u.Role != RoleLibrarian || u.Name != "Dave" || u.Picture != "https://img/d.png" || !u.LastLoginAt.Equal(svc.now()) {
		t.Fatalf("linked user: %+v", u)
	}
	var n int
	_ = database.QueryRowContext(ctx, "SELECT count(*) FROM identities WHERE user_id = ?", existing.ID).Scan(&n)
	if n != 2 {
		t.Fatalf("want 2 identities, got %d", n)
	}
	// Now the google identity resolves directly, even if the email changed at the IdP.
	u2, err := svc.linkIdentity(ctx, "google", "g-sub", "dave@other.org", "David", "", "")
	if err != nil || u2.ID != existing.ID || u2.Name != "David" || u2.Email != "dave@example.com" {
		t.Fatalf("by subject: %v %+v", err, u2)
	}
	// A different subject with a new email creates a new user with the local part as name.
	u3, err := svc.linkIdentity(ctx, "google", "g-sub-2", "erin@example.com", "", "", "")
	if err != nil || u3.ID == existing.ID || u3.Name != "erin" || u3.Role != RoleMember {
		t.Fatalf("new user: %v %+v", err, u3)
	}
}

func TestSweep(t *testing.T) {
	ctx := context.Background()
	svc, database := newTestService(t, nil)
	u, _ := svc.DevLogin(ctx, "s@example.com", "", "")
	live, _, _ := svc.CreateSession(ctx, u, "", "")
	stale, _, _ := svc.CreateSession(ctx, u, "", "")
	// Push one session into the past by editing its row directly.
	if _, err := database.ExecContext(ctx, "UPDATE sessions SET expires_at = ? WHERE id = ?", formatTime(svc.now().Add(-time.Second)), hashToken(stale)); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.insertChallenge(ctx, database, "old", "oidc", "{}", svc.now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.insertChallenge(ctx, database, "fresh", "oidc", "{}", svc.now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	var sessions, challenges int
	_ = database.QueryRowContext(ctx, "SELECT count(*) FROM sessions").Scan(&sessions)
	_ = database.QueryRowContext(ctx, "SELECT count(*) FROM challenges").Scan(&challenges)
	if sessions != 1 || challenges != 1 {
		t.Fatalf("after sweep: sessions=%d challenges=%d", sessions, challenges)
	}
	if _, _, err := svc.Authenticate(ctx, live); err != nil {
		t.Fatalf("live session swept: %v", err)
	}

	// RunSweeper exits promptly when the context is cancelled.
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { svc.RunSweeper(runCtx, time.Millisecond); close(done) }()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunSweeper did not stop")
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"  A@B.co ": "a@b.co",
		"a@b":       "a@b",
		"":          "",
		"a":         "",
		"@b":        "",
		"a@":        "",
		"a@b@c":     "",
		"a b@c":     "",
	}
	for in, want := range cases {
		if got := normalizeEmail(in); got != want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
	if fallbackName(" ", "x@y") != "x" || fallbackName("Z", "x@y") != "Z" {
		t.Fatal("fallbackName")
	}
}
