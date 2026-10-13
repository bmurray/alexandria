// Code generated from Pkl module `alexandria.auth.Auth`. DO NOT EDIT.
package auth

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/auth/provider"
)

// Authentication and roles.
type Auth struct {
	// Sign-in methods, in the order the login page shows them.
	Providers []provider.Provider `pkl:"providers"`

	Session Session `pkl:"session"`

	// Emails that become `admin` on first sign-in (and are promoted on sign-in
	// if they are not already admin). Everyone else starts as `member`.
	Admins []string `pkl:"admins"`

	// If non-empty, only emails on these domains may sign in.
	AllowedEmailDomains []string `pkl:"allowedEmailDomains"`
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a Auth
func LoadFromPath(ctx context.Context, path string) (ret Auth, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a Auth
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (Auth, error) {
	var ret Auth
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
