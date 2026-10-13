// Package credential resolves Pkl Credential values (config/credential/*.pkl)
// into secret strings at runtime: Static (dev), File (dev, optionally one key
// of a JSON file) and K8sSecret (a projected Secret directory).
package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	gen "github.com/bmurray/alexandria/internal/gen/credential"
	"github.com/bmurray/alexandria/internal/gen/credential/file"
	"github.com/bmurray/alexandria/internal/gen/credential/k8ssecret"
	"github.com/bmurray/alexandria/internal/gen/credential/static"
)

// Resolver turns Credential config values into secret strings. Files are read
// at most once per resolver.
type Resolver struct {
	mu    sync.Mutex
	files map[string]string
}

// NewResolver creates a resolver.
func NewResolver() *Resolver {
	return &Resolver{files: map[string]string{}}
}

// Resolve returns the secret value for c. A nil credential resolves to "".
func (r *Resolver) Resolve(ctx context.Context, c gen.Credential) (string, error) {
	if c == nil {
		return "", nil
	}
	switch c.GetType() {
	case "static":
		v, ok := c.(static.Static)
		if !ok {
			return "", fmt.Errorf("credential: type static is %T", c)
		}
		return v.GetValue(), nil
	case "k8s_secret":
		v, ok := c.(k8ssecret.K8sSecret)
		if !ok {
			return "", fmt.Errorf("credential: type k8s_secret is %T", c)
		}
		return r.readFile(filepath.Join(v.GetDir(), v.GetKey()))
	case "file":
		v, ok := c.(file.File)
		if !ok {
			return "", fmt.Errorf("credential: type file is %T", c)
		}
		path := expandHome(v.GetPath())
		if k := v.GetJsonKey(); k != nil && *k != "" {
			return r.readJSONKey(path, *k)
		}
		return r.readFile(path)
	default:
		return "", fmt.Errorf("unknown credential type %q (%T)", c.GetType(), c)
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func (r *Resolver) readRaw(path string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.files[path]; ok {
		return v, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read credential file: %w", err)
	}
	r.files[path] = string(data)
	return string(data), nil
}

func (r *Resolver) readFile(path string) (string, error) {
	raw, err := r.readRaw(path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(raw, "\r\n"), nil
}

func (r *Resolver) readJSONKey(path, key string) (string, error) {
	raw, err := r.readRaw(path)
	if err != nil {
		return "", err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return "", fmt.Errorf("credential file %s: %w", filepath.Base(path), err)
	}
	v, ok := m[key]
	if !ok {
		return "", fmt.Errorf("credential file %s: no key %q", filepath.Base(path), key)
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", fmt.Errorf("credential file %s: key %q is not a string", filepath.Base(path), key)
	}
	return s, nil
}
