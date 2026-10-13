// Code generated from Pkl module `alexandria.credential.K8sSecret`. DO NOT EDIT.
package k8ssecret

import (
	"context"

	"github.com/apple/pkl-go/pkl"
	"github.com/bmurray/alexandria/internal/gen/credential"
)

type K8sSecret interface {
	credential.Credential

	GetDir() string

	GetKey() string
}

var _ K8sSecret = K8sSecretImpl{}

// Kubernetes Secret projected as a directory: each data key is a file.
// Resolves to the contents of `<dir>/<key>` with trailing newlines trimmed.
// The Secret is never read through the API.
type K8sSecretImpl struct {
	// Discriminator. Each implementation fixes it.
	Type string `pkl:"type"`

	// Mount path of the projected Secret. The k8s deploy schema derives its
	// volume mountPath from this default; keep them in step.
	Dir string `pkl:"dir"`

	// Secret data key, i.e. the filename under `dir`. No path separators.
	Key string `pkl:"key"`
}

// Discriminator. Each implementation fixes it.
func (rcv K8sSecretImpl) GetType() string {
	return rcv.Type
}

// Mount path of the projected Secret. The k8s deploy schema derives its
// volume mountPath from this default; keep them in step.
func (rcv K8sSecretImpl) GetDir() string {
	return rcv.Dir
}

// Secret data key, i.e. the filename under `dir`. No path separators.
func (rcv K8sSecretImpl) GetKey() string {
	return rcv.Key
}

// LoadFromPath loads the pkl module at the given path and evaluates it into a K8sSecret
func LoadFromPath(ctx context.Context, path string) (ret K8sSecret, err error) {
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

// Load loads the pkl module at the given source and evaluates it with the given evaluator into a K8sSecret
func Load(ctx context.Context, evaluator pkl.Evaluator, source *pkl.ModuleSource) (K8sSecret, error) {
	var ret K8sSecretImpl
	err := evaluator.EvaluateModule(ctx, source, &ret)
	return ret, err
}
