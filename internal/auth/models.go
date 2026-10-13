// Package auth implements accounts, external identities (OIDC), the Dev
// sign-in, cookie sessions and roles.
package auth

import (
	"errors"
	"time"
)

// Role orders what a user may do: admin > librarian > member.
type Role string

// Roles.
const (
	RoleMember    Role = "member"
	RoleLibrarian Role = "librarian"
	RoleAdmin     Role = "admin"
)

var roleRank = map[Role]int{RoleMember: 1, RoleLibrarian: 2, RoleAdmin: 3}

// Valid reports whether r is a known role.
func (r Role) Valid() bool { _, ok := roleRank[r]; return ok }

// AtLeast reports whether r grants everything min does.
func (r Role) AtLeast(min Role) bool { return roleRank[r] >= roleRank[min] }

// User is a signed-in person.
type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	Picture     string    `json:"picture"`
	Role        Role      `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

// Session is one browser's cookie session.
type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	UserAgent  string    `json:"userAgent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// OIDCProvider is one configured OpenID Connect issuer (plain values; the
// client secret is already resolved).
type OIDCProvider struct {
	Name            string
	DisplayName     string
	Issuer          string
	ClientID        string
	ClientSecret    string
	Scopes          []string
	ExtraAuthParams map[string]string
}

// Config is the auth configuration as plain values (internal/app converts
// the generated Pkl types into this).
type Config struct {
	// PublicURL is the origin the browser uses; redirect URIs derive from it.
	PublicURL string
	OIDC      []OIDCProvider
	// DevLogin enables POST /api/auth/dev/login (local development, e2e).
	DevLogin        bool
	DevDisplayName  string
	CookieName      string
	SessionDuration time.Duration
	SecureCookie    bool
	// Admins become admin on sign-in. Everyone else starts as member.
	Admins              []string
	AllowedEmailDomains []string
}

// Sentinel errors mapped to HTTP statuses by the handlers.
var (
	ErrUnauthenticated  = errors.New("not signed in")
	ErrForbidden        = errors.New("forbidden")
	ErrNotFound         = errors.New("not found")
	ErrUnknownProvider  = errors.New("unknown sign-in provider")
	ErrChallengeExpired = errors.New("sign-in attempt expired; try again")
	ErrEmailNotAllowed  = errors.New("that email domain may not sign in")
	ErrInvalidEmail     = errors.New("invalid email address")
	ErrInvalidRole      = errors.New("invalid role")
	ErrDevLoginDisabled = errors.New("development sign-in is not enabled")
	// ErrInvalidReturnTo is a returnTo that is not a site-relative path.
	ErrInvalidReturnTo = errors.New("returnTo must be a path on this site")
	// ErrSelfDemotion is an admin trying to lower their own role (409).
	ErrSelfDemotion = errors.New("an admin may not change their own role")
	// ErrExchangeFailed wraps a failed OIDC code exchange or ID-token check.
	ErrExchangeFailed = errors.New("sign-in with the identity provider failed")
	// ErrProviderUnavailable wraps a failed OIDC discovery (502).
	ErrProviderUnavailable = errors.New("the identity provider is not reachable")
)
