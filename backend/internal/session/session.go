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

	"github.com/shiroha-a/town/internal/settings"
)

// CookieName is the login cookie.
const CookieName = "town_session"

// defaultTTL is used when no settings store is wired (テストや単発の計算)。
// 実際の値は管理画面の「ログインの有効期限」(settings.SessionTTLDays)。
const defaultTTL = 30 * 24 * time.Hour

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
	// settings holds the admin-editable game settings (有効期限をここから読む)。
	// nil なら defaultTTL を使う。
	settings *settings.Store
}

// New returns a session store. st may be nil (その場合は既定の有効期限)。
func New(pool *pgxpool.Pool, secure bool, st *settings.Store) *Store {
	return &Store{pool: pool, Secure: secure, settings: st}
}

// TTL is how long a login lasts without being used. 遊ぶたびに延びる(ローリング)
// ので、これは「最後に遊んでからログインし直しになるまで」の長さ。
func (s *Store) TTL() time.Duration {
	if s.settings == nil {
		return defaultTTL
	}
	days := s.settings.Get().SessionTTLDays
	if days < 1 {
		return defaultTTL
	}
	return time.Duration(days) * 24 * time.Hour
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
		hash(token), playerID, s.TTL().String()); err != nil {
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
// slid reports that the expiry moved; the caller must then re-send the cookie,
// otherwise the browser drops it on the original expiry no matter how long the
// row lives(これを怠ると「遊び続けても切れない」が成立しない)。
func (s *Store) Lookup(ctx context.Context, token string) (playerID int64, slid bool, err error) {
	if token == "" {
		return 0, false, ErrNotFound
	}
	h := hash(token)
	var lastSeen time.Time
	err = s.pool.QueryRow(ctx,
		`SELECT player_id, last_seen_at FROM sessions
		 WHERE token_hash = $1 AND expires_at > now()`, h).Scan(&playerID, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, ErrNotFound
	}
	if err != nil {
		return 0, false, fmt.Errorf("lookup session: %w", err)
	}
	if time.Since(lastSeen) > touchInterval {
		if _, e := s.pool.Exec(ctx,
			`UPDATE sessions SET last_seen_at = now(), expires_at = now() + $2::interval
			 WHERE token_hash = $1`, h, s.TTL().String()); e == nil {
			slid = true
		}
	}
	return playerID, slid, nil
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
		Expires:  time.Now().Add(s.TTL()),
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
