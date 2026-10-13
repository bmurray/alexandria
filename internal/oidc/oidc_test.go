package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func signToken(t *testing.T, key *rsa.PrivateKey, kid string, payload map[string]any) string {
	t.Helper()
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	body, _ := json.Marshal(payload)
	signing := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newTestProvider(t *testing.T, issuer string, key *rsa.PrivateKey) *Provider {
	t.Helper()
	p := New(Config{Name: "test", Issuer: issuer, ClientID: "client-1", ClientSecret: "s", RedirectURL: "http://localhost/cb"})
	p.keys = map[string]*rsa.PublicKey{"k1": &key.PublicKey}
	p.keysAt = time.Now()
	p.keysFetch = func(context.Context) error { return errors.New("no network in tests") }
	return p
}

func TestVerifyIDToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := newTestProvider(t, "https://accounts.google.com", key)
	now := time.Now().Unix()
	base := map[string]any{
		"iss": "https://accounts.google.com", "sub": "123", "aud": "client-1",
		"exp": now + 300, "iat": now, "nonce": "n1",
		"email": "Alice@Example.com", "email_verified": true, "name": "Alice",
	}

	id, err := p.verifyIDToken(context.Background(), signToken(t, key, "k1", base), "n1")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Email != "alice@example.com" || !id.EmailVerified || id.Subject != "123" {
		t.Fatalf("unexpected identity %+v", id)
	}

	cases := map[string]func(m map[string]any){
		"wrong aud":   func(m map[string]any) { m["aud"] = "other" },
		"expired":     func(m map[string]any) { m["exp"] = now - 600 },
		"bad nonce":   func(m map[string]any) { m["nonce"] = "x" },
		"wrong iss":   func(m map[string]any) { m["iss"] = "https://evil.example" },
		"missing sub": func(m map[string]any) { delete(m, "sub") },
	}
	for name, mutate := range cases {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		mutate(m)
		if _, err := p.verifyIDToken(context.Background(), signToken(t, key, "k1", m), "n1"); !errors.Is(err, ErrInvalidIDToken) {
			t.Errorf("%s: expected ErrInvalidIDToken, got %v", name, err)
		}
	}

	// Signature from a different key must fail.
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	if _, err := p.verifyIDToken(context.Background(), signToken(t, other, "k1", base), "n1"); !errors.Is(err, ErrInvalidIDToken) {
		t.Errorf("foreign key: expected ErrInvalidIDToken, got %v", err)
	}
	// Unknown kid triggers a refresh which fails in tests.
	if _, err := p.verifyIDToken(context.Background(), signToken(t, key, "k9", base), "n1"); err == nil {
		t.Error("unknown kid: expected error")
	}
}

func TestMicrosoftMultiTenantIssuerAndAudArray(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	p := newTestProvider(t, "https://login.microsoftonline.com/common/v2.0", key)
	now := time.Now().Unix()
	tok := signToken(t, key, "k1", map[string]any{
		"iss": "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0",
		"sub": "abc", "aud": []string{"client-1"}, "exp": now + 60, "iat": now,
		"preferred_username": "bob@contoso.com", "name": "Bob",
	})
	id, err := p.verifyIDToken(context.Background(), tok, "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.Email != "bob@contoso.com" || id.EmailVerified {
		t.Fatalf("unexpected identity %+v", id)
	}
	if !matchIssuer("https://login.microsoftonline.com/{tenantid}/v2.0", "https://login.microsoftonline.com/abc/v2.0") {
		t.Error("templated issuer should match")
	}
	if matchIssuer("https://login.microsoftonline.com/{tenantid}/v2.0", "https://login.microsoftonline.com//v2.0") {
		t.Error("empty tenant should not match")
	}
}

func TestPKCE(t *testing.T) {
	v, c := PKCE([]byte("0123456789abcdef0123456789abcdef"))
	if v == "" || c == "" || v == c {
		t.Fatal("bad pkce pair")
	}
	sum := sha256.Sum256([]byte(v))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != c {
		t.Fatal("challenge mismatch")
	}
}
