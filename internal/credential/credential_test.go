package credential

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bmurray/alexandria/internal/gen/credential/file"
	"github.com/bmurray/alexandria/internal/gen/credential/k8ssecret"
	"github.com/bmurray/alexandria/internal/gen/credential/static"
)

func TestResolve(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "creds.json"), []byte(`{"client_secret":"s3cret","n":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewResolver()

	if v, err := r.Resolve(ctx, nil); err != nil || v != "" {
		t.Fatalf("nil: %q %v", v, err)
	}
	if v, err := r.Resolve(ctx, static.StaticImpl{Type: "static", Value: "x"}); err != nil || v != "x" {
		t.Fatalf("static: %q %v", v, err)
	}
	if v, err := r.Resolve(ctx, k8ssecret.K8sSecretImpl{Type: "k8s_secret", Dir: dir, Key: "token"}); err != nil || v != "abc" {
		t.Fatalf("k8s: %q %v", v, err)
	}
	if v, err := r.Resolve(ctx, file.FileImpl{Type: "file", Path: filepath.Join(dir, "token")}); err != nil || v != "abc" {
		t.Fatalf("file: %q %v", v, err)
	}
	key := "client_secret"
	if v, err := r.Resolve(ctx, file.FileImpl{Type: "file", Path: filepath.Join(dir, "creds.json"), JsonKey: &key}); err != nil || v != "s3cret" {
		t.Fatalf("json: %q %v", v, err)
	}
	bad := "n"
	if _, err := r.Resolve(ctx, file.FileImpl{Type: "file", Path: filepath.Join(dir, "creds.json"), JsonKey: &bad}); err == nil {
		t.Fatal("non-string key should fail")
	}
	if _, err := r.Resolve(ctx, file.FileImpl{Type: "file", Path: filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("missing file should fail")
	}
}
