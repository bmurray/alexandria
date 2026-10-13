// Package config loads the application configuration through pkl-loader.
//
// The only process environment variable is CONFIG_FILE. It may point at a
// .pkl file (evaluated against the embedded schema), a pre-rendered .pklbin,
// or a .json / .yaml file. Credential and provider values are one-of-many
// interfaces, which JSON/YAML cannot carry; ship .pkl or .pklbin.
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pklloader "github.com/bmurray/pkl-loader"

	schema "github.com/bmurray/alexandria/config"
	"github.com/bmurray/alexandria/internal/gen/appconfig"

	// Register polymorphic Pkl type mappings (init side effects).
	_ "github.com/bmurray/alexandria/internal/gen/ai"
	_ "github.com/bmurray/alexandria/internal/gen/ai/provider/anthropic"
	_ "github.com/bmurray/alexandria/internal/gen/auth"
	_ "github.com/bmurray/alexandria/internal/gen/auth/provider/dev"
	_ "github.com/bmurray/alexandria/internal/gen/auth/provider/oidc"
	_ "github.com/bmurray/alexandria/internal/gen/credential/file"
	_ "github.com/bmurray/alexandria/internal/gen/credential/k8ssecret"
	_ "github.com/bmurray/alexandria/internal/gen/credential/static"
	_ "github.com/bmurray/alexandria/internal/gen/otel"
)

// EnvVar is the only environment variable the application reads.
const EnvVar = "CONFIG_FILE"

// Path returns the configured config file path or an error if unset.
func Path() (string, error) {
	p := strings.TrimSpace(os.Getenv(EnvVar))
	if p == "" {
		return "", fmt.Errorf("%s is not set", EnvVar)
	}
	return p, nil
}

// Load reads and validates the application configuration at path.
func Load(ctx context.Context, path string) (*appconfig.AppConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("config path is empty")
	}
	abs := path
	if a, err := filepath.Abs(path); err == nil {
		abs = a
	}
	cfg, err := pklloader.Load[appconfig.AppConfig](ctx, abs, pklloader.WithSchema(schema.SchemaFS))
	if err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

func validate(c *appconfig.AppConfig) error {
	if c.Server.Port == 0 {
		return fmt.Errorf("server.port must be set")
	}
	if c.Database.Path == "" {
		return fmt.Errorf("database.path must be set")
	}
	seen := map[string]bool{}
	for _, p := range c.Auth.Providers {
		if p == nil {
			continue
		}
		if seen[p.GetName()] {
			return fmt.Errorf("auth.providers: duplicate name %q", p.GetName())
		}
		seen[p.GetName()] = true
	}
	names := map[string]bool{}
	for _, p := range c.Ai.Providers {
		if p == nil {
			continue
		}
		if names[p.GetName()] {
			return fmt.Errorf("ai.providers: duplicate name %q", p.GetName())
		}
		names[p.GetName()] = true
	}
	if c.Ai.Enabled && len(c.Ai.Providers) == 0 {
		return fmt.Errorf("ai.enabled requires at least one provider")
	}
	return nil
}
