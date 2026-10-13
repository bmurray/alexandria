// Code generated from Pkl module `alexandria.credential.Static`. DO NOT EDIT.
package static

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/credential"
)

type Static interface {
	credential.Credential

	GetValue() string
}

var _ Static = StaticImpl{}

// A literal secret value. Development only: a production config containing
// a Static lands the value in a ConfigMap.
type StaticImpl struct {
	// Discriminator. Each implementation fixes it.
	Type string `pkl:"type"`

	Value string `pkl:"value"`
}

// Discriminator. Each implementation fixes it.
func (rcv StaticImpl) GetType() string {
	return rcv.Type
}

func (rcv StaticImpl) GetValue() string {
	return rcv.Value
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a Static
func LoadFromPath(ctx context.Context, path string) (ret Static, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a Static
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (Static, error) {
	var ret StaticImpl
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
