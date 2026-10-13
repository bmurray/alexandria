package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/bmurray/alexandria/internal/id"
	"github.com/bmurray/alexandria/internal/oidc"
)

const (
	challengeKindOIDC = "oidc"
	challengeTTL      = 10 * time.Minute
)

// exchanger is the slice of *oidc.Provider the service uses. Tests swap in a
// fake so the callback can be exercised without an identity provider.
type exchanger interface {
	AuthURL(ctx context.Context, state, nonce, codeChallenge string) (string, error)
	Exchange(ctx context.Context, code, codeVerifier, nonce string) (*oidc.Identity, error)
}

var _ exchanger = (*oidc.Provider)(nil)

// oidcChallenge is the JSON payload of an "oidc" challenge row, keyed by the
// state parameter.
type oidcChallenge struct {
	Provider string `json:"provider"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	ReturnTo string `json:"returnTo"`
}

// redirectURL is where the IdP sends the browser back for provider name.
func (s *Service) redirectURL(name string) string {
	return s.cfg.PublicURL + "/api/auth/oidc/" + name + "/callback"
}

// OIDCStart begins the authorization-code flow with PKCE. It stores the
// state, nonce, verifier and returnTo as a challenge and returns the IdP URL
// to redirect the browser to. returnTo must be a site-relative path.
func (s *Service) OIDCStart(ctx context.Context, provider, returnTo string) (string, error) {
	p, ok := s.providers[provider]
	if !ok {
		return "", ErrUnknownProvider
	}
	returnTo, err := cleanReturnTo(returnTo)
	if err != nil {
		return "", err
	}
	state := id.Token(32)
	nonce := id.Token(32)
	verifier, challenge := oidc.PKCE(id.Bytes(32))

	payload, err := json.Marshal(oidcChallenge{Provider: provider, Verifier: verifier, Nonce: nonce, ReturnTo: returnTo})
	if err != nil {
		return "", fmt.Errorf("auth: encode challenge: %w", err)
	}
	// Discovery may need the network; do it before writing the row so a
	// failure leaves nothing behind.
	authURL, err := p.AuthURL(ctx, state, nonce, challenge)
	if err != nil {
		s.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("provider", provider)))
		return "", fmt.Errorf("%w: %s: %w", ErrProviderUnavailable, provider, err)
	}
	if err := s.store.insertChallenge(ctx, s.db, state, challengeKindOIDC, string(payload), s.now().Add(challengeTTL)); err != nil {
		return "", fmt.Errorf("auth: store challenge: %w", err)
	}
	return authURL, nil
}

// OIDCCallback completes the flow: it consumes the challenge for state,
// redeems code with the provider, verifies the ID token and signs the person
// in. It returns the user and the returnTo path captured at start.
func (s *Service) OIDCCallback(ctx context.Context, provider, state, code string) (*User, string, error) {
	ctx, span := s.tracer.Start(ctx, "auth.OIDCCallback")
	defer span.End()
	span.SetAttributes(attribute.String("auth.provider", provider))

	p, ok := s.providers[provider]
	if !ok {
		return nil, "", ErrUnknownProvider
	}
	if state == "" || code == "" {
		return nil, "", ErrChallengeExpired
	}
	payload, err := s.store.consumeChallenge(ctx, s.db, state, challengeKindOIDC, s.now())
	if err != nil {
		if !errors.Is(err, ErrChallengeExpired) {
			err = fmt.Errorf("auth: load challenge: %w", err)
		}
		return nil, "", err
	}
	var ch oidcChallenge
	if err := json.Unmarshal([]byte(payload), &ch); err != nil || ch.Provider != provider {
		// A state issued for another provider is as good as unknown.
		return nil, "", ErrChallengeExpired
	}

	ident, err := p.Exchange(ctx, code, ch.Verifier, ch.Nonce)
	if err != nil {
		s.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("provider", provider)))
		slog.WarnContext(ctx, "auth: oidc exchange failed", slog.String("provider", provider), slog.String("error", err.Error()))
		return nil, "", fmt.Errorf("%w: %w", ErrExchangeFailed, err)
	}
	u, err := s.signIn(ctx, signIn{
		Provider: provider,
		Subject:  ident.Subject,
		Email:    ident.Email,
		Name:     ident.Name,
		Picture:  ident.Picture,
	})
	if err != nil {
		return nil, "", err
	}
	return u, ch.ReturnTo, nil
}

// cleanReturnTo accepts only site-relative paths so the callback can never
// redirect off-site. Empty means "/".
func cleanReturnTo(p string) (string, error) {
	if p == "" {
		return "/", nil
	}
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") ||
		strings.ContainsAny(p, "\r\n") {
		return "", ErrInvalidReturnTo
	}
	return p, nil
}
