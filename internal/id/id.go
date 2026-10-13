// Package id generates random identifiers without external dependencies.
package id

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"fmt"
)

var enc = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// New returns a 20-character lowercase base32 identifier (100 random bits).
func New() string {
	return enc.EncodeToString(Bytes(13))[:20]
}

// Bytes returns n cryptographically random bytes.
func Bytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand: %v", err))
	}
	return b
}

// Token returns a URL-safe random token encoding n random bytes.
func Token(n int) string {
	return base64.RawURLEncoding.EncodeToString(Bytes(n))
}
