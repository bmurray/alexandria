// Code generated from Pkl module `alexandria.auth.provider.Dev`. DO NOT EDIT.
package dev

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/auth/provider"
)

type Dev interface {
	provider.Provider

	GetAllowPublic() bool
}

var _ Dev = DevImpl{}

// Development sign-in: `POST /api/auth/dev/login` with any email, name and
// role creates or signs in that user. Never enable this on a public host;
// startup refuses it when `server.publicUrl` is not localhost unless
// `allowPublic` is set (e2e in CI runs on localhost, so it never is).
type DevImpl struct {
	Type string `pkl:"type"`

	// Short identifier used in URLs (`/api/auth/oidc/<name>/start`) and on the
	// identities table. Lowercase letters, digits and dashes.
	Name string `pkl:"name"`

	// Label on the sign-in button.
	DisplayName string `pkl:"displayName"`

	AllowPublic bool `pkl:"allowPublic"`

	Enabled bool `pkl:"enabled"`
}

func (rcv DevImpl) GetType() string {
	return rcv.Type
}

// Short identifier used in URLs (`/api/auth/oidc/<name>/start`) and on the
// identities table. Lowercase letters, digits and dashes.
func (rcv DevImpl) GetName() string {
	return rcv.Name
}

// Label on the sign-in button.
func (rcv DevImpl) GetDisplayName() string {
	return rcv.DisplayName
}

func (rcv DevImpl) GetAllowPublic() bool {
	return rcv.AllowPublic
}

func (rcv DevImpl) GetEnabled() bool {
	return rcv.Enabled
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a Dev
func LoadFromPath(ctx context.Context, path string) (ret Dev, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a Dev
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (Dev, error) {
	var ret DevImpl
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
