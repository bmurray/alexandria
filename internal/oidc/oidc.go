// Package oidc is a small OpenID Connect relying-party client using discovery,
// PKCE and RS256 ID-token verification. It depends only on the standard library.
package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// Errors returned by the client.
var (
	ErrExchangeFailed  = errors.New("oidc: token exchange failed")
	ErrInvalidIDToken  = errors.New("oidc: invalid id_token")
	ErrDiscoveryFailed = errors.New("oidc: discovery failed")
)

// Config describes a provider.
type Config struct {
	// Name is a short identifier such as "google".
	Name string
	// Issuer URL. Discovery is fetched from Issuer + "/.well-known/openid-configuration".
	// Microsoft's multi-tenant issuer contains "{tenantid}"; see matchIssuer.
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	// ExtraAuthParams are appended to the authorization request (e.g. Google "hd").
	ExtraAuthParams map[string]string
	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// Identity is what we learn about the user from the ID token.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Issuer        string
}

// Provider is a configured relying-party client. Safe for concurrent use.
type Provider struct {
	cfg  Config
	http *http.Client

	mu        sync.Mutex
	disc      *discovery
	discAt    time.Time
	keys      map[string]*rsa.PublicKey
	keysAt    time.Time
	keysFetch func(ctx context.Context) error
}

type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

const discoveryTTL = 24 * time.Hour

// New creates a provider. Discovery is fetched lazily on first use.
func New(cfg Config) *Provider {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "email", "profile"}
	}
	p := &Provider{cfg: cfg, http: hc, keys: map[string]*rsa.PublicKey{}}
	p.keysFetch = p.fetchJWKS
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return p.cfg.Name }

// PKCE returns a code verifier and its S256 challenge.
func PKCE(random []byte) (verifier, challenge string) {
	verifier = base64.RawURLEncoding.EncodeToString(random)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthURL builds the authorization redirect for the given state, nonce and PKCE challenge.
func (p *Provider) AuthURL(ctx context.Context, state, nonce, codeChallenge string) (string, error) {
	d, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("client_id", p.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("scope", strings.Join(p.cfg.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	for k, v := range p.cfg.ExtraAuthParams {
		q.Set(k, v)
	}
	sep := "?"
	if strings.Contains(d.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return d.AuthorizationEndpoint + sep + q.Encode(), nil
}

// Exchange redeems the authorization code and verifies the returned ID token.
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, nonce string) (*Identity, error) {
	ctx, span := otel.Tracer("alexandria/oidc").Start(ctx, "oidc.Exchange")
	defer span.End()
	span.SetAttributes(attribute.String("oidc.provider", p.cfg.Name))

	d, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.http.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "token request")
		return nil, fmt.Errorf("%w: %v", ErrExchangeFailed, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		span.SetStatus(codes.Error, "token status")
		return nil, fmt.Errorf("%w: status %d: %s", ErrExchangeFailed, resp.StatusCode, truncate(string(body), 300))
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.IDToken == "" {
		return nil, fmt.Errorf("%w: no id_token in response", ErrExchangeFailed)
	}
	id, err := p.verifyIDToken(ctx, tok.IDToken, nonce)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "id_token")
		return nil, err
	}
	return id, nil
}

func (p *Provider) discover(ctx context.Context) (*discovery, error) {
	p.mu.Lock()
	if p.disc != nil && time.Since(p.discAt) < discoveryTTL {
		d := p.disc
		p.mu.Unlock()
		return d, nil
	}
	p.mu.Unlock()

	u := strings.TrimSuffix(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDiscoveryFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d from %s", ErrDiscoveryFailed, resp.StatusCode, u)
	}
	var d discovery
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&d); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDiscoveryFailed, err)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return nil, fmt.Errorf("%w: incomplete document from %s", ErrDiscoveryFailed, u)
	}
	p.mu.Lock()
	p.disc, p.discAt = &d, time.Now()
	p.mu.Unlock()
	return &d, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (p *Provider) fetchJWKS(ctx context.Context) error {
	d, err := p.discover(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.JWKSURI, nil)
	if err != nil {
		return err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("oidc: jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: jwks status %d", resp.StatusCode)
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&set); err != nil {
		return fmt.Errorf("oidc: jwks decode: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := rsaFromJWK(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pub
	}
	p.mu.Lock()
	p.keys, p.keysAt = keys, time.Now()
	p.mu.Unlock()
	return nil
}

func rsaFromJWK(k jwk) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	e := new(big.Int).SetBytes(eb)
	if !e.IsInt64() || e.Int64() > int64(^uint32(0)) {
		return nil, errors.New("exponent too large")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(e.Int64())}, nil
}

// key returns the signing key for kid, refreshing JWKS at most once when unknown.
func (p *Provider) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	p.mu.Lock()
	k, ok := p.keys[kid]
	stale := time.Since(p.keysAt) > time.Minute
	p.mu.Unlock()
	if ok {
		return k, nil
	}
	if !stale {
		return nil, fmt.Errorf("%w: unknown key id %q", ErrInvalidIDToken, kid)
	}
	if err := p.keysFetch(ctx); err != nil {
		return nil, err
	}
	p.mu.Lock()
	k, ok = p.keys[kid]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: unknown key id %q", ErrInvalidIDToken, kid)
	}
	return k, nil
}

type claims struct {
	Iss           string          `json:"iss"`
	Sub           string          `json:"sub"`
	Aud           json.RawMessage `json:"aud"`
	Exp           int64           `json:"exp"`
	Iat           int64           `json:"iat"`
	Nonce         string          `json:"nonce"`
	Email         string          `json:"email"`
	EmailVerified any             `json:"email_verified"`
	Name          string          `json:"name"`
	Picture       string          `json:"picture"`
	// Microsoft often carries the sign-in email here instead of "email".
	PreferredUsername string `json:"preferred_username"`
}

func (p *Provider) verifyIDToken(ctx context.Context, raw, nonce string) (*Identity, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: malformed", ErrInvalidIDToken)
	}
	hdrBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: header", ErrInvalidIDToken)
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return nil, fmt.Errorf("%w: header", ErrInvalidIDToken)
	}
	if hdr.Alg != "RS256" {
		return nil, fmt.Errorf("%w: unsupported alg %q", ErrInvalidIDToken, hdr.Alg)
	}
	pub, err := p.key(ctx, hdr.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: signature", ErrInvalidIDToken)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return nil, fmt.Errorf("%w: bad signature", ErrInvalidIDToken)
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: payload", ErrInvalidIDToken)
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, fmt.Errorf("%w: claims", ErrInvalidIDToken)
	}
	if !matchIssuer(p.cfg.Issuer, c.Iss) {
		return nil, fmt.Errorf("%w: issuer %q", ErrInvalidIDToken, c.Iss)
	}
	if !audienceContains(c.Aud, p.cfg.ClientID) {
		return nil, fmt.Errorf("%w: audience", ErrInvalidIDToken)
	}
	now := time.Now()
	if c.Exp == 0 || now.After(time.Unix(c.Exp, 0).Add(time.Minute)) {
		return nil, fmt.Errorf("%w: expired", ErrInvalidIDToken)
	}
	if c.Iat != 0 && time.Unix(c.Iat, 0).After(now.Add(5*time.Minute)) {
		return nil, fmt.Errorf("%w: issued in the future", ErrInvalidIDToken)
	}
	if nonce != "" && c.Nonce != nonce {
		return nil, fmt.Errorf("%w: nonce mismatch", ErrInvalidIDToken)
	}
	if c.Sub == "" {
		return nil, fmt.Errorf("%w: missing sub", ErrInvalidIDToken)
	}

	email := c.Email
	if email == "" && strings.Contains(c.PreferredUsername, "@") {
		email = c.PreferredUsername
	}
	return &Identity{
		Subject:       c.Sub,
		Email:         strings.ToLower(strings.TrimSpace(email)),
		EmailVerified: boolClaim(c.EmailVerified),
		Name:          c.Name,
		Picture:       c.Picture,
		Issuer:        c.Iss,
	}, nil
}

// matchIssuer compares the configured issuer with the token issuer. Microsoft's
// "common"/"organizations"/"consumers" discovery documents publish a templated
// issuer with "{tenantid}"; tokens carry the real tenant id in that position.
func matchIssuer(configured, actual string) bool {
	configured = strings.TrimSuffix(configured, "/")
	actual = strings.TrimSuffix(actual, "/")
	if configured == actual {
		return true
	}
	if i := strings.Index(configured, "{tenantid}"); i >= 0 {
		prefix, suffix := configured[:i], configured[i+len("{tenantid}"):]
		return strings.HasPrefix(actual, prefix) && strings.HasSuffix(actual, suffix) &&
			len(actual) > len(prefix)+len(suffix)
	}
	// Multi-tenant Microsoft: the configured issuer names the pseudo tenant but
	// tokens name the real tenant GUID.
	for _, pseudo := range []string{"common", "organizations", "consumers"} {
		tmpl := "https://login.microsoftonline.com/" + pseudo + "/v2.0"
		if configured == tmpl {
			return strings.HasPrefix(actual, "https://login.microsoftonline.com/") && strings.HasSuffix(actual, "/v2.0")
		}
	}
	return false
}

func audienceContains(raw json.RawMessage, clientID string) bool {
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single == clientID
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == clientID {
				return true
			}
		}
	}
	return false
}

func boolClaim(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true")
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
