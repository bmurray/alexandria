package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/bmurray/alexandria/internal/id"
	"github.com/bmurray/alexandria/internal/oidc"
)

// Defaults applied by NewService when Config leaves a field zero.
const (
	DefaultCookieName      = "alexandria_session"
	DefaultSessionDuration = 30 * 24 * time.Hour
	DefaultDevDisplayName  = "Development"

	// devProvider is the identity provider name used by DevLogin.
	devProvider = "dev"
	// sessionTouchInterval bounds how often Authenticate writes last_seen_at.
	sessionTouchInterval = 5 * time.Minute
)

// ProviderInfo describes one sign-in option to the web app.
type ProviderInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	// Type is "oidc" or "dev".
	Type string `json:"type"`
}

// Service implements accounts, sessions and roles over SQLite. It satisfies
// middleware.Authenticator. Safe for concurrent use.
type Service struct {
	cfg   Config
	db    *sql.DB
	store *store

	// providers holds the OIDC relying-party clients keyed by name, in
	// configuration order. Values are exchangers so tests can fake the IdP.
	providers     map[string]exchanger
	providerOrder []string
	admins        map[string]bool
	domains       []string

	// now is overridable in tests.
	now func() time.Time

	tracer   trace.Tracer
	logins   metric.Int64Counter
	failures metric.Int64Counter
}

// NewService validates cfg, builds one OIDC client per configured provider
// (discovery is lazy, so this does no network I/O) and returns the service.
func NewService(ctx context.Context, db *sql.DB, cfg Config) (*Service, error) {
	if db == nil {
		return nil, errors.New("auth: nil database")
	}
	if cfg.CookieName == "" {
		cfg.CookieName = DefaultCookieName
	}
	if cfg.SessionDuration <= 0 {
		cfg.SessionDuration = DefaultSessionDuration
	}
	if cfg.DevDisplayName == "" {
		cfg.DevDisplayName = DefaultDevDisplayName
	}
	cfg.PublicURL = strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/")
	if len(cfg.OIDC) > 0 && cfg.PublicURL == "" {
		return nil, errors.New("auth: publicURL is required when OIDC providers are configured")
	}

	s := &Service{
		cfg:       cfg,
		db:        db,
		store:     &store{db: db},
		providers: make(map[string]exchanger, len(cfg.OIDC)),
		admins:    make(map[string]bool, len(cfg.Admins)),
		now:       time.Now,
		tracer:    otel.Tracer("alexandria/auth"),
	}
	for _, p := range cfg.OIDC {
		name := strings.TrimSpace(p.Name)
		switch {
		case name == "":
			return nil, errors.New("auth: OIDC provider without a name")
		case name == devProvider:
			return nil, fmt.Errorf("auth: OIDC provider name %q is reserved", name)
		case strings.ContainsAny(name, "/?#"):
			return nil, fmt.Errorf("auth: OIDC provider name %q must be URL-safe", name)
		case p.Issuer == "" || p.ClientID == "":
			return nil, fmt.Errorf("auth: OIDC provider %q needs an issuer and clientId", name)
		}
		if _, dup := s.providers[name]; dup {
			return nil, fmt.Errorf("auth: duplicate OIDC provider %q", name)
		}
		s.providers[name] = oidc.New(oidc.Config{
			Name:            name,
			Issuer:          p.Issuer,
			ClientID:        p.ClientID,
			ClientSecret:    p.ClientSecret,
			RedirectURL:     s.redirectURL(name),
			Scopes:          p.Scopes,
			ExtraAuthParams: p.ExtraAuthParams,
		})
		s.providerOrder = append(s.providerOrder, name)
	}
	for _, a := range cfg.Admins {
		if e := normalizeEmail(a); e != "" {
			s.admins[e] = true
		}
	}
	for _, d := range cfg.AllowedEmailDomains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" {
			s.domains = append(s.domains, d)
		}
	}

	meter := otel.Meter("alexandria/auth")
	var err error
	if s.logins, err = meter.Int64Counter("alexandria.auth.logins",
		metric.WithDescription("Successful sign-ins by provider")); err != nil {
		return nil, fmt.Errorf("auth: metrics: %w", err)
	}
	if s.failures, err = meter.Int64Counter("alexandria.auth.failures",
		metric.WithDescription("Failed sign-in attempts by provider")); err != nil {
		return nil, fmt.Errorf("auth: metrics: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("auth: database: %w", err)
	}
	return s, nil
}

// CookieName is the session cookie's name.
func (s *Service) CookieName() string { return s.cfg.CookieName }

// CookieSecure reports whether the session cookie must carry the Secure flag.
func (s *Service) CookieSecure() bool { return s.cfg.SecureCookie }

// SessionDuration is how long a session (and its cookie) lives.
func (s *Service) SessionDuration() time.Duration { return s.cfg.SessionDuration }

// PublicURL is the origin browsers use, without a trailing slash.
func (s *Service) PublicURL() string { return s.cfg.PublicURL }

// DevLoginEnabled reports whether the Dev sign-in is on.
func (s *Service) DevLoginEnabled() bool { return s.cfg.DevLogin }

// Providers lists the sign-in options: OIDC providers in configuration
// order, then "dev" when enabled.
func (s *Service) Providers() []ProviderInfo {
	out := make([]ProviderInfo, 0, len(s.cfg.OIDC)+1)
	for _, p := range s.cfg.OIDC {
		display := p.DisplayName
		if display == "" {
			display = p.Name
		}
		out = append(out, ProviderInfo{Name: p.Name, DisplayName: display, Type: "oidc"})
	}
	if s.cfg.DevLogin {
		out = append(out, ProviderInfo{Name: devProvider, DisplayName: s.cfg.DevDisplayName, Type: devProvider})
	}
	return out
}

// --- sessions --------------------------------------------------------------

// hashToken maps a bearer token to its sessions.id. Only the hash is stored,
// so a database leak does not yield usable cookies.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Authenticate resolves a session cookie token. Expired or unknown tokens
// return ErrUnauthenticated. last_seen_at is refreshed at most once every
// five minutes to keep the hot path read-mostly.
func (s *Service) Authenticate(ctx context.Context, token string) (*User, *Session, error) {
	if token == "" {
		return nil, nil, ErrUnauthenticated
	}
	sess, user, err := s.store.getSessionWithUser(ctx, s.db, hashToken(token))
	if errors.Is(err, ErrNotFound) {
		return nil, nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, nil, fmt.Errorf("auth: load session: %w", err)
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) {
		return nil, nil, ErrUnauthenticated
	}
	if now.Sub(sess.LastSeenAt) >= sessionTouchInterval {
		if err := s.store.touchSession(ctx, s.db, sess.ID, now); err != nil {
			// A failed touch must not sign the user out; log and carry on.
			slog.WarnContext(ctx, "auth: touch session", slog.String("error", err.Error()))
		} else {
			sess.LastSeenAt = now.UTC().Truncate(time.Second)
		}
	}
	return user, sess, nil
}

// CreateSession issues a new session for u and returns the bearer token to
// put in the cookie. The token is never stored; only its SHA-256 is.
func (s *Service) CreateSession(ctx context.Context, u *User, userAgent, ip string) (string, *Session, error) {
	if u == nil || u.ID == "" {
		return "", nil, errors.New("auth: create session: no user")
	}
	token := id.Token(32)
	now := s.now().UTC().Truncate(time.Second)
	sess := &Session{
		ID:         hashToken(token),
		UserID:     u.ID,
		UserAgent:  truncateString(userAgent, 512),
		IP:         truncateString(ip, 64),
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.cfg.SessionDuration),
	}
	if err := s.store.insertSession(ctx, s.db, sess); err != nil {
		return "", nil, fmt.Errorf("auth: create session: %w", err)
	}
	return token, sess, nil
}

// Logout deletes the session for token. Unknown tokens are not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := s.store.deleteSession(ctx, s.db, hashToken(token)); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	return nil
}

// LogoutAll deletes every session belonging to userID.
func (s *Service) LogoutAll(ctx context.Context, userID string) error {
	if err := s.store.deleteUserSessions(ctx, s.db, userID); err != nil {
		return fmt.Errorf("auth: logout all: %w", err)
	}
	return nil
}

// --- users and roles -------------------------------------------------------

// GetUser returns one user or ErrNotFound.
func (s *Service) GetUser(ctx context.Context, userID string) (*User, error) {
	return s.store.getUser(ctx, s.db, userID)
}

// ListUsers returns every user ordered by creation time.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	return s.store.listUsers(ctx, s.db)
}

// SetRole changes userID's role. Only admins may call it, the role must be
// valid, and an admin may not demote themselves (so a library always keeps
// at least the admin making the change).
func (s *Service) SetRole(ctx context.Context, actor *User, userID string, role Role) (*User, error) {
	if actor == nil {
		return nil, ErrUnauthenticated
	}
	if !actor.Role.AtLeast(RoleAdmin) {
		return nil, ErrForbidden
	}
	if !role.Valid() {
		return nil, ErrInvalidRole
	}
	if actor.ID == userID && role != RoleAdmin {
		return nil, ErrSelfDemotion
	}
	if err := s.store.updateUserRole(ctx, s.db, userID, role); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("auth: set role: %w", err)
	}
	slog.InfoContext(ctx, "auth: role changed",
		slog.String("actor_id", actor.ID), slog.String("user_id", userID), slog.String("role", string(role)))
	return s.store.getUser(ctx, s.db, userID)
}

// --- sign-in ---------------------------------------------------------------

// signIn is what every provider funnels through once it has verified who the
// person is. It links the (provider, subject) identity to a user, creating
// one when needed, applies the email policy and the admin list, records the
// login and returns the current user row.
type signIn struct {
	Provider string
	Subject  string
	Email    string
	Name     string
	Picture  string
	// Role, when set, is written explicitly (Dev sign-in). The admin list
	// still wins. Empty keeps an existing user's role or defaults to member.
	Role Role
}

func (s *Service) signIn(ctx context.Context, in signIn) (*User, error) {
	ctx, span := s.tracer.Start(ctx, "auth.signIn")
	defer span.End()
	span.SetAttributes(attribute.String("auth.provider", in.Provider))

	email, err := s.checkEmail(in.Email)
	if err != nil {
		s.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("provider", in.Provider)))
		return nil, err
	}
	if in.Role != "" && !in.Role.Valid() {
		return nil, ErrInvalidRole
	}
	if in.Subject == "" {
		return nil, fmt.Errorf("auth: %s sign-in without a subject", in.Provider)
	}

	u, err := s.linkIdentity(ctx, in.Provider, in.Subject, email, in.Name, in.Picture, in.Role)
	if err != nil {
		s.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("provider", in.Provider)))
		return nil, err
	}
	s.logins.Add(ctx, 1, metric.WithAttributes(attribute.String("provider", in.Provider)))
	slog.InfoContext(ctx, "auth: sign-in",
		slog.String("provider", in.Provider), slog.String("user_id", u.ID), slog.String("role", string(u.Role)))
	return u, nil
}

// linkIdentity finds the user for (provider, subject); failing that, the
// user with the same email (and links the identity to them); failing that,
// it creates a new user. In every case it refreshes the profile, applies the
// admin list and stamps last_login_at. Runs in one transaction.
func (s *Service) linkIdentity(ctx context.Context, provider, subject, email, name, picture string, role Role) (*User, error) {
	now := s.now().UTC().Truncate(time.Second)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	var u *User
	userID, err := s.store.findIdentityUser(ctx, tx, provider, subject)
	switch {
	case err == nil:
		if u, err = s.store.getUser(ctx, tx, userID); err != nil {
			return nil, fmt.Errorf("auth: identity user: %w", err)
		}
	case errors.Is(err, ErrNotFound):
		u, err = s.store.getUserByEmail(ctx, tx, email)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("auth: user by email: %w", err)
		}
		if u == nil {
			u = &User{ID: id.New(), Email: email, Role: RoleMember, CreatedAt: now, LastLoginAt: now}
			if role != "" {
				u.Role = role
			}
			u.Name = fallbackName(name, email)
			u.Picture = picture
			if s.admins[email] {
				u.Role = RoleAdmin
			}
			if err := s.store.insertUser(ctx, tx, u); err != nil {
				return nil, fmt.Errorf("auth: create user: %w", err)
			}
		}
		if err := s.store.insertIdentity(ctx, tx, id.New(), u.ID, provider, subject, now); err != nil {
			return nil, fmt.Errorf("auth: link identity: %w", err)
		}
	default:
		return nil, fmt.Errorf("auth: find identity: %w", err)
	}

	// Refresh what the provider told us, but never blank out a field.
	if name = strings.TrimSpace(name); name != "" {
		u.Name = name
	}
	if u.Name == "" {
		u.Name = fallbackName("", email)
	}
	if picture != "" {
		u.Picture = picture
	}
	if role != "" {
		u.Role = role
	}
	if s.admins[email] {
		u.Role = RoleAdmin
	}
	u.LastLoginAt = now
	if err := s.store.updateUserSignIn(ctx, tx, u); err != nil {
		return nil, fmt.Errorf("auth: update user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("auth: commit: %w", err)
	}
	return u, nil
}

// checkEmail normalizes and validates an email and enforces the allowed
// domain list.
func (s *Service) checkEmail(raw string) (string, error) {
	email := normalizeEmail(raw)
	if email == "" {
		return "", ErrInvalidEmail
	}
	if len(s.domains) > 0 {
		domain := email[strings.LastIndex(email, "@")+1:]
		allowed := false
		for _, d := range s.domains {
			if domain == d {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", ErrEmailNotAllowed
		}
	}
	return email, nil
}

// normalizeEmail lowercases and trims raw and returns "" when it is not a
// plausible address: exactly one "@" with non-empty parts and no whitespace.
func normalizeEmail(raw string) string {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return ""
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return ""
	}
	return email
}

// fallbackName returns name, or the email's local part when name is blank.
func fallbackName(name, email string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	local, _, _ := strings.Cut(email, "@")
	return local
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// --- maintenance -----------------------------------------------------------

// Sweep deletes expired sessions and challenges.
func (s *Service) Sweep(ctx context.Context) error {
	now := s.now()
	sessions, err := s.store.deleteExpiredSessions(ctx, s.db, now)
	if err != nil {
		return fmt.Errorf("auth: sweep sessions: %w", err)
	}
	challenges, err := s.store.deleteExpiredChallenges(ctx, s.db, now)
	if err != nil {
		return fmt.Errorf("auth: sweep challenges: %w", err)
	}
	if sessions+challenges > 0 {
		slog.DebugContext(ctx, "auth: swept", slog.Int64("sessions", sessions), slog.Int64("challenges", challenges))
	}
	return nil
}

// RunSweeper calls Sweep every interval until ctx is done. Run it in its own
// goroutine.
func (s *Service) RunSweeper(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = time.Hour
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
				slog.ErrorContext(ctx, "auth: sweep", slog.String("error", err.Error()))
			}
		}
	}
}
