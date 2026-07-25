// Package profile shows a resident's Misskey account inside the game and lets
// one resident follow another from the town's profile facility.
//
// Design: .tmp/design_prof.md
//
// Two外部呼び出しの性質がまったく違うので分けて扱う:
//   - プロフィール取得(users/show)は認証不要。相手のインスタンスに直接聞く。
//   - フォローは必ず「閲覧者のインスタンス上で、閲覧者のトークン」で行う。
//     別インスタンスの相手は ap/show で解決が要り、これが30回/時と厳しいので
//     解決結果を必ずキャッシュする。
package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/miauth"
	"github.com/shiroha-a/town/internal/player"
)

// cacheTTL is how long a cached profile is served without re-fetching. Counts
// (notes/followers) drift, so the fetch time is shown alongside them.
const cacheTTL = 6 * time.Hour

// ErrNoProfile means the player has never had a Misskey profile fetched and the
// instance could not be reached now.
var ErrNoProfile = errors.New("profile unavailable")

// Service reads and caches Misskey profiles.
type Service struct {
	pool    *pgxpool.Pool
	mi      *miauth.Client
	players *player.Service
}

// New builds the service.
func New(pool *pgxpool.Pool, mi *miauth.Client, players *player.Service) *Service {
	return &Service{pool: pool, mi: mi, players: players}
}

// Profile is a resident's Misskey account as the game shows it.
type Profile struct {
	PlayerID       int64     `json:"player_id"`
	Username       string    `json:"username"`
	Host           string    `json:"host"`
	Acct           string    `json:"acct"`
	Name           string    `json:"name"`
	AvatarURL      string    `json:"avatar_url"`
	BannerURL      string    `json:"banner_url"`
	Description    string    `json:"description"`
	FollowersCount int       `json:"followers_count"`
	FollowingCount int       `json:"following_count"`
	NotesCount     int       `json:"notes_count"`
	IsBot          bool      `json:"is_bot"`
	IsCat          bool      `json:"is_cat"`
	IsLocked       bool      `json:"is_locked"`
	FetchedAt      time.Time `json:"fetched_at"`
	ProfileURL     string    `json:"profile_url"`
	// Stale marks a profile served from cache because the instance could not be
	// reached. The UI says so rather than pretending the counts are current.
	Stale bool `json:"stale"`
}

// Get returns the player's profile, fetching it when the cache is cold or old.
// A failed fetch falls back to the cached copy (marked Stale) so one instance
// being down does not blank out the page.
func (s *Service) Get(ctx context.Context, playerID int64) (*Profile, error) {
	cached, hasCache, err := s.cached(ctx, playerID)
	if err != nil {
		return nil, err
	}
	if hasCache && time.Since(cached.FetchedAt) < cacheTTL {
		return cached, nil
	}
	fresh, err := s.fetch(ctx, playerID)
	if err != nil {
		if hasCache {
			cached.Stale = true
			return cached, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrNoProfile, err)
	}
	return fresh, nil
}

// Refresh fetches the profile regardless of the cache age.
func (s *Service) Refresh(ctx context.Context, playerID int64) (*Profile, error) {
	return s.fetch(ctx, playerID)
}

func (s *Service) cached(ctx context.Context, playerID int64) (*Profile, bool, error) {
	var (
		p    Profile
		host *string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT username, host, name, avatar_url, banner_url, description,
		        followers_count, following_count, notes_count,
		        is_bot, is_cat, is_locked, fetched_at
		   FROM misskey_profiles WHERE player_id = $1`, playerID).
		Scan(&p.Username, &host, &p.Name, &p.AvatarURL, &p.BannerURL, &p.Description,
			&p.FollowersCount, &p.FollowingCount, &p.NotesCount,
			&p.IsBot, &p.IsCat, &p.IsLocked, &p.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read profile cache: %w", err)
	}
	p.PlayerID = playerID
	if host != nil {
		p.Host = *host
	}
	s.decorate(ctx, &p, playerID)
	return &p, true, nil
}

// decorate fills the fields derived from the player's own instance.
func (s *Service) decorate(ctx context.Context, p *Profile, playerID int64) {
	home, err := s.players.Get(ctx, playerID)
	if err != nil {
		return
	}
	// host は「そのインスタンスから見て」なので、ローカルユーザーだとNULLで返る。
	// 表示用には住民が所属するインスタンスを補う。
	if p.Host == "" {
		p.Host = home.InstanceHost
	}
	p.Acct = miauth.Acct(p.Username, p.Host, home.InstanceHost)
	p.ProfileURL = miauth.ProfileURL(p.Host, p.Username)
}

func (s *Service) fetch(ctx context.Context, playerID int64) (*Profile, error) {
	home, err := s.players.Get(ctx, playerID)
	if err != nil {
		return nil, err
	}
	if home.InstanceHost == "" || home.RemoteUserID == "" {
		return nil, fmt.Errorf("Misskeyアカウントが紐付いていません。")
	}
	u, err := s.mi.ShowUser(ctx, home.InstanceHost, "", home.RemoteUserID)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO misskey_profiles (player_id, username, host, name, avatar_url, banner_url,
		     description, followers_count, following_count, notes_count, is_bot, is_cat, is_locked, fetched_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, now())
		 ON CONFLICT (player_id) DO UPDATE SET
		     username = EXCLUDED.username, host = EXCLUDED.host, name = EXCLUDED.name,
		     avatar_url = EXCLUDED.avatar_url, banner_url = EXCLUDED.banner_url,
		     description = EXCLUDED.description, followers_count = EXCLUDED.followers_count,
		     following_count = EXCLUDED.following_count, notes_count = EXCLUDED.notes_count,
		     is_bot = EXCLUDED.is_bot, is_cat = EXCLUDED.is_cat, is_locked = EXCLUDED.is_locked,
		     fetched_at = now()`,
		playerID, u.Username, nullable(u.Host), u.Name, u.AvatarURL, u.BannerURL,
		u.Description, u.FollowersCount, u.FollowingCount, u.NotesCount,
		u.IsBot, u.IsCat, u.IsLocked); err != nil {
		return nil, fmt.Errorf("save profile: %w", err)
	}
	p := &Profile{
		PlayerID: playerID, Username: u.Username, Host: u.Host, Name: u.Name,
		AvatarURL: u.AvatarURL, BannerURL: u.BannerURL, Description: u.Description,
		FollowersCount: u.FollowersCount, FollowingCount: u.FollowingCount,
		NotesCount: u.NotesCount, IsBot: u.IsBot, IsCat: u.IsCat, IsLocked: u.IsLocked,
		FetchedAt: time.Now(),
	}
	s.decorate(ctx, p, playerID)
	return p, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
