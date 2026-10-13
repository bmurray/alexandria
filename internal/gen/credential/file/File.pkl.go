// Code generated from Pkl module `alexandria.credential.File`. DO NOT EDIT.
package file

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/credential"
)

type File interface {
	credential.Credential

	GetPath() string

	GetJsonKey() *string
}

var _ File = FileImpl{}

// A file on disk, optionally one key of a JSON object in it. For secrets kept
// outside the repository on a developer machine, e.g.
// `~/.config/secrets/alexandria/oidc.json` with `client_id` / `client_secret`.
type FileImpl struct {
	// Discriminator. Each implementation fixes it.
	Type string `pkl:"type"`

	// Absolute path, or `~/...` for the home directory.
	Path string `pkl:"path"`

	// If set, the file is JSON and this top-level key holds the secret.
	JsonKey *string `pkl:"jsonKey"`
}

// Discriminator. Each implementation fixes it.
func (rcv FileImpl) GetType() string {
	return rcv.Type
}

// Absolute path, or `~/...` for the home directory.
func (rcv FileImpl) GetPath() string {
	return rcv.Path
}

// If set, the file is JSON and this top-level key holds the secret.
func (rcv FileImpl) GetJsonKey() *string {
	return rcv.JsonKey
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a File
func LoadFromPath(ctx context.Context, path string) (ret File, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a File
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (File, error) {
	var ret FileImpl
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
