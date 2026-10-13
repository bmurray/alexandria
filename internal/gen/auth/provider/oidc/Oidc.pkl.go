// Code generated from Pkl module `alexandria.auth.provider.Oidc`. DO NOT EDIT.
package oidc

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/auth/provider"
	"github.com/bmurray/alexandria/internal/gen/credential"
)

type Oidc interface {
	provider.Provider

	GetIssuer() string

	GetClientId() string

	GetClientSecret() credential.Credential

	GetScopes() []string

	GetExtraAuthParams() map[string]string
}

var _ Oidc = OidcImpl{}

// OpenID Connect sign-in: authorization code + PKCE, RS256 ID token verified
// against the issuer's JWKS. Google: issuer "https://accounts.google.com".
// Redirect URI registered with the provider must be
// `<server.publicUrl>/api/auth/oidc/<name>/callback`.
type OidcImpl struct {
	Type string `pkl:"type"`

	Issuer string `pkl:"issuer"`

	ClientId string `pkl:"clientId"`

	ClientSecret credential.Credential `pkl:"clientSecret"`

	Scopes []string `pkl:"scopes"`

	// Extra authorization-request parameters, e.g. Google's `hd` to restrict to
	// one Workspace domain, or `prompt = "select_account"`.
	ExtraAuthParams map[string]string `pkl:"extraAuthParams"`

	// Short identifier used in URLs (`/api/auth/oidc/<name>/start`) and on the
	// identities table. Lowercase letters, digits and dashes.
	Name string `pkl:"name"`

	// Label on the sign-in button.
	DisplayName string `pkl:"displayName"`

	Enabled bool `pkl:"enabled"`
}

func (rcv OidcImpl) GetType() string {
	return rcv.Type
}

func (rcv OidcImpl) GetIssuer() string {
	return rcv.Issuer
}

func (rcv OidcImpl) GetClientId() string {
	return rcv.ClientId
}

func (rcv OidcImpl) GetClientSecret() credential.Credential {
	return rcv.ClientSecret
}

func (rcv OidcImpl) GetScopes() []string {
	return rcv.Scopes
}

// Extra authorization-request parameters, e.g. Google's `hd` to restrict to
// one Workspace domain, or `prompt = "select_account"`.
func (rcv OidcImpl) GetExtraAuthParams() map[string]string {
	return rcv.ExtraAuthParams
}

// Short identifier used in URLs (`/api/auth/oidc/<name>/start`) and on the
// identities table. Lowercase letters, digits and dashes.
func (rcv OidcImpl) GetName() string {
	return rcv.Name
}

// Label on the sign-in button.
func (rcv OidcImpl) GetDisplayName() string {
	return rcv.DisplayName
}

func (rcv OidcImpl) GetEnabled() bool {
	return rcv.Enabled
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a Oidc
func LoadFromPath(ctx context.Context, path string) (ret Oidc, err error) {
	evaluator, err := pkl.NewEvaluator(ctx, pkl.PreconfiguredOptions)
	if err != nil {
		return ret, err
	}
	defer func() {
		cerr := evaluator.Close()
		if err == nil {
			err = cerr
		}
	}()
	ret, err = Load(ctx, evaluator, pkl.FileSource(path))
	return ret, err
}

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a Oidc
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (Oidc, error) {
	var ret OidcImpl
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
