package auth

import (
	"context"
	"strings"
)

// DevLogin signs in as any email without a password. It exists for local
// development and the e2e suite and answers ErrDevLoginDisabled unless
// Config.DevLogin is set; the deployment must never enable it on a public
// hostname.
//
// role, when given, is written to the user (creating or updating them), so a
// test can become a librarian in one call. It defaults to member for new
// users and is left alone for existing ones. Emails in Config.Admins are
// admins regardless.
func (s *Service) DevLogin(ctx context.Context, email, name string, role Role) (*User, error) {
	if !s.cfg.DevLogin {
		return nil, ErrDevLoginDisabled
	}
	if role != "" && !role.Valid() {
		return nil, ErrInvalidRole
	}
	email = normalizeEmail(email)
	if email == "" {
		return nil, ErrInvalidEmail
	}
	return s.signIn(ctx, signIn{
		Provider: devProvider,
		Subject:  email,
		Email:    email,
		Name:     strings.TrimSpace(name),
		Role:     role,
	})
}
