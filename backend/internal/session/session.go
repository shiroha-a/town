// Package session issues and validates login sessions. The cookie carries a
// random token; only its SHA-256 is stored, so a database leak cannot be
// replayed as a login.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CookieName is the login cookie.
const CookieName = "town_session"

// TTL is how long a login lasts without being used. Every use slides it
// forward, so continuous play never expires.
const TTL = 30 * 24 * time.Hour

// touchInterval throttles last_seen_at writes (same idea as players.last_seen_at).
const touchInterval = 30 * time.Second

// ErrNotFound means the token is unknown or expired.
var ErrNotFound = errors.New("session not found")

// Store manages sessions.
type Store struct {
	pool *pgxpool.Pool
	// Secure marks cookies Secure. Off for plain-HTTP development access
	// (Tailscale IP など)。
	Secure bool
}

// New returns a session store.
func New(pool *pgxpool.Pool, secure bool) *Store {
	return &Store{pool: pool, Secure: secure}
}

func hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Issue creates a session for the player and returns the raw token.
func (s *Store) Issue(ctx context.Context, playerID int64) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, player_id, expires_at) VALUES ($1, $2, now() + $3::interval)`,
		hash(token), playerID, TTL.String()); err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return token, nil
}

// Lookup resolves a token to its player. Using a session slides its expiry
// forward (rolling TTL) so an active player is never forced to log in again:
// each MiAuth login mints a new access token on the instance and Misskey does
// not let us revoke it programmatically (i/revoke-token is secure:true, so an
// access token cannot call it), which means re-logins leave orphaned tokens in
// the user's connected-apps list. Fewer logins is the only lever we have.
// The write is throttled to touchInterval so it is not one UPDATE per request.
func (s *Store) Lookup(ctx context.Context, token string) (int64, error) {
	if token == "" {
		return 0, ErrNotFound
	}
	h := hash(token)
	var playerID int64
	var lastSeen time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT player_id, last_seen_at FROM sessions
		 WHERE token_hash = $1 AND expires_at > now()`, h).Scan(&playerID, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lookup session: %w", err)
	}
	if time.Since(lastSeen) > touchInterval {
		_, _ = s.pool.Exec(ctx,
			`UPDATE sessions SET last_seen_at = now(), expires_at = now() + $2::interval
			 WHERE token_hash = $1`, h, TTL.String())
	}
	return playerID, nil
}

// Revoke deletes one session (logout).
func (s *Store) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash(token)); err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	return nil
}

// RevokeAll deletes every session of a player (logout everywhere).
func (s *Store) RevokeAll(ctx context.Context, playerID int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE player_id = $1`, playerID); err != nil {
		return fmt.Errorf("revoke all: %w", err)
	}
	return nil
}

// SetCookie writes the login cookie.
func (s *Store) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(TTL),
	})
}

// ClearCookie expires the login cookie.
func (s *Store) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// TokenFrom pulls the raw token out of a request's cookie.
func TokenFrom(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
