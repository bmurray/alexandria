package config

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

// The checked-in dev and e2e configs must always evaluate against the schema.
func TestLoadRepoConfigs(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	for _, name := range []string{"dev.pkl", "e2e.pkl"} {
		cfg, err := Load(context.Background(), filepath.Join(root, "configs", name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if cfg.Server.Port != 24380 {
			t.Fatalf("%s: port = %d", name, cfg.Server.Port)
		}
		if len(cfg.Auth.Providers) == 0 || cfg.Auth.Providers[0].GetType() != "dev" {
			t.Fatalf("%s: expected the dev provider first", name)
		}
	}
}
