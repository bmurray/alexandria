package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Times are stored as RFC3339 UTC text at second precision. A fixed-width
// format keeps lexicographic comparison in SQL (expires_at <= ?) correct.
const timeLayout = time.RFC3339

func formatTime(t time.Time) string { return t.UTC().Truncate(time.Second).Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
	}
	return t, nil
}

// querier is satisfied by both *sql.DB and *sql.Tx so store methods can run
// inside or outside a transaction. The pool is capped at one connection, so
// code inside a transaction must only touch the transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// store is the SQLite persistence for users, identities, sessions and
// challenges. It knows nothing about policy; Service does.
type store struct {
	db *sql.DB
}

// --- users -----------------------------------------------------------------

const userColumns = "id, email, name, picture, role, created_at, last_login_at"

func scanUser(row interface{ Scan(dest ...any) error }) (*User, error) {
	var u User
	var created, lastLogin string
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.Picture, &u.Role, &created, &lastLogin); err != nil {
		return nil, err
	}
	var err error
	if u.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if u.LastLoginAt, err = parseTime(lastLogin); err != nil {
		return nil, err
	}
	return &u, nil
}

func (st *store) getUser(ctx context.Context, q querier, id string) (*User, error) {
	u, err := scanUser(q.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (st *store) getUserByEmail(ctx context.Context, q querier, email string) (*User, error) {
	u, err := scanUser(q.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE email = ?", email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (st *store) listUsers(ctx context.Context, q querier) ([]User, error) {
	rows, err := q.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY created_at, rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (st *store) insertUser(ctx context.Context, q querier, u *User) error {
	_, err := q.ExecContext(ctx,
		"INSERT INTO users ("+userColumns+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.Email, u.Name, u.Picture, u.Role, formatTime(u.CreatedAt), formatTime(u.LastLoginAt))
	return err
}

// updateUserSignIn records a sign-in: profile fields, role and last_login_at.
func (st *store) updateUserSignIn(ctx context.Context, q querier, u *User) error {
	_, err := q.ExecContext(ctx,
		"UPDATE users SET name = ?, picture = ?, role = ?, last_login_at = ? WHERE id = ?",
		u.Name, u.Picture, u.Role, formatTime(u.LastLoginAt), u.ID)
	return err
}

func (st *store) updateUserRole(ctx context.Context, q querier, id string, role Role) error {
	res, err := q.ExecContext(ctx, "UPDATE users SET role = ? WHERE id = ?", role, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- identities ------------------------------------------------------------

// findIdentityUser returns the user id linked to (provider, subject) or ErrNotFound.
func (st *store) findIdentityUser(ctx context.Context, q querier, provider, subject string) (string, error) {
	var userID string
	err := q.QueryRowContext(ctx,
		"SELECT user_id FROM identities WHERE provider = ? AND subject = ?", provider, subject).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return userID, err
}

func (st *store) insertIdentity(ctx context.Context, q querier, id, userID, provider, subject string, now time.Time) error {
	_, err := q.ExecContext(ctx,
		"INSERT INTO identities (id, user_id, provider, subject, created_at) VALUES (?, ?, ?, ?, ?)",
		id, userID, provider, subject, formatTime(now))
	return err
}

// --- sessions --------------------------------------------------------------

func (st *store) insertSession(ctx context.Context, q querier, s *Session) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, user_agent, ip, created_at, last_seen_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.UserID, s.UserAgent, s.IP, formatTime(s.CreatedAt), formatTime(s.LastSeenAt), formatTime(s.ExpiresAt))
	return err
}

// getSessionWithUser loads a session and its user in one query. Expiry is
// checked by the caller so the policy lives in one place.
func (st *store) getSessionWithUser(ctx context.Context, q querier, id string) (*Session, *User, error) {
	row := q.QueryRowContext(ctx,
		`SELECT s.id, s.user_id, s.user_agent, s.ip, s.created_at, s.last_seen_at, s.expires_at,
		        u.id, u.email, u.name, u.picture, u.role, u.created_at, u.last_login_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.id = ?`, id)
	var s Session
	var u User
	var sCreated, sSeen, sExpires, uCreated, uLogin string
	err := row.Scan(&s.ID, &s.UserID, &s.UserAgent, &s.IP, &sCreated, &sSeen, &sExpires,
		&u.ID, &u.Email, &u.Name, &u.Picture, &u.Role, &uCreated, &uLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	for _, f := range []struct {
		dst *time.Time
		src string
	}{{&s.CreatedAt, sCreated}, {&s.LastSeenAt, sSeen}, {&s.ExpiresAt, sExpires}, {&u.CreatedAt, uCreated}, {&u.LastLoginAt, uLogin}} {
		if *f.dst, err = parseTime(f.src); err != nil {
			return nil, nil, err
		}
	}
	return &s, &u, nil
}

func (st *store) touchSession(ctx context.Context, q querier, id string, seen time.Time) error {
	_, err := q.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE id = ?", formatTime(seen), id)
	return err
}

func (st *store) deleteSession(ctx context.Context, q querier, id string) error {
	_, err := q.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	return err
}

func (st *store) deleteUserSessions(ctx context.Context, q querier, userID string) error {
	_, err := q.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

func (st *store) deleteExpiredSessions(ctx context.Context, q querier, now time.Time) (int64, error) {
	res, err := q.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", formatTime(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- challenges ------------------------------------------------------------

func (st *store) insertChallenge(ctx context.Context, q querier, id, kind, payload string, expires time.Time) error {
	_, err := q.ExecContext(ctx,
		"INSERT INTO challenges (id, kind, payload, expires_at) VALUES (?, ?, ?, ?)",
		id, kind, payload, formatTime(expires))
	return err
}

// consumeChallenge deletes the challenge and returns its payload. A missing,
// wrong-kind or expired challenge is ErrChallengeExpired: the caller cannot
// tell the cases apart and must not be able to.
func (st *store) consumeChallenge(ctx context.Context, q querier, id, kind string, now time.Time) (string, error) {
	var payload, expires string
	err := q.QueryRowContext(ctx,
		"SELECT payload, expires_at FROM challenges WHERE id = ? AND kind = ?", id, kind).Scan(&payload, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrChallengeExpired
	}
	if err != nil {
		return "", err
	}
	if _, err := q.ExecContext(ctx, "DELETE FROM challenges WHERE id = ?", id); err != nil {
		return "", err
	}
	exp, err := parseTime(expires)
	if err != nil {
		return "", err
	}
	if !now.Before(exp) {
		return "", ErrChallengeExpired
	}
	return payload, nil
}

func (st *store) deleteExpiredChallenges(ctx context.Context, q querier, now time.Time) (int64, error) {
	res, err := q.ExecContext(ctx, "DELETE FROM challenges WHERE expires_at <= ?", formatTime(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
