package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shiroha-a/town/internal/miauth"
	"github.com/shiroha-a/town/internal/player"
)

// FollowState is the viewer's relation to the profile being shown.
type FollowState struct {
	Self bool `json:"self"`
	// Known is false when the viewer's instance could not be asked (it does not
	// know the account yet, or the call failed). The button is still offered —
	// the follow itself resolves the account and reports "already following".
	Known     bool `json:"known"`
	Following bool `json:"following"`
	Pending   bool `json:"pending"`
	// CanFollow is false when we hold no usable Misskey token for the viewer.
	CanFollow bool `json:"can_follow"`
	// FallbackURL is the viewer's own instance's remote-follow page, used when
	// the API route is unavailable (no token, rate limited, resolve failed).
	FallbackURL string `json:"fallback_url"`
}

// FollowResult is the outcome of a follow attempt.
type FollowResult struct {
	Following   bool   `json:"following"`
	Pending     bool   `json:"pending"`
	Message     string `json:"message"`
	FallbackURL string `json:"fallback_url"`
}

// State reports whether the viewer already follows the target, asking the
// viewer's own instance (users/show has no rate limit, so this can run on every
// profile view). The lookup doubles as the resolution step: the id it returns is
// the one a follow needs, so it is cached here.
func (s *Service) State(ctx context.Context, viewerID, targetID int64) (*FollowState, error) {
	st := &FollowState{Self: viewerID == targetID}
	if st.Self || viewerID == 0 {
		return st, nil
	}
	viewer, target, err := s.pair(ctx, viewerID, targetID)
	if err != nil {
		return nil, err
	}
	st.FallbackURL = s.fallbackURL(ctx, viewer, targetID)

	token, err := s.players.MisskeyToken(ctx, viewerID)
	if err != nil || token == "" {
		return st, nil
	}
	st.CanFollow = true

	u, err := s.lookup(ctx, viewer, target, targetID, token)
	if err != nil {
		// 相手のインスタンスとまだ疎通が無い等。ボタンは出したまま、
		// 「フォロー中」の判定だけ諦める(実行時に改めて判定される)。
		return st, nil
	}
	st.Known = true
	st.Following = u.IsFollowing
	st.Pending = u.HasPendingFollowRequestFromYou
	return st, nil
}

// Follow makes the viewer follow the target on the viewer's own instance.
func (s *Service) Follow(ctx context.Context, viewerID, targetID int64) (*FollowResult, error) {
	return s.follow(ctx, viewerID, targetID, false)
}

// Unfollow undoes Follow.
func (s *Service) Unfollow(ctx context.Context, viewerID, targetID int64) (*FollowResult, error) {
	return s.follow(ctx, viewerID, targetID, true)
}

func (s *Service) follow(ctx context.Context, viewerID, targetID int64, undo bool) (*FollowResult, error) {
	if viewerID == targetID {
		return nil, fmt.Errorf("自分自身はフォローできません。")
	}
	viewer, target, err := s.pair(ctx, viewerID, targetID)
	if err != nil {
		return nil, err
	}
	fallback := s.fallbackURL(ctx, viewer, targetID)

	token, err := s.players.MisskeyToken(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return &FollowResult{
			Message:     "Misskeyの連携が切れています。Misskey側の画面からフォローしてください。",
			FallbackURL: fallback,
		}, nil
	}

	u, err := s.lookup(ctx, viewer, target, targetID, token)
	if err != nil {
		return &FollowResult{Message: followErrMessage(err), FallbackURL: fallback}, nil
	}
	userID := u.ID

	if undo {
		if err := s.mi.Unfollow(ctx, viewer.InstanceHost, token, userID); err != nil {
			return &FollowResult{Following: true, Message: followErrMessage(err), FallbackURL: fallback}, nil
		}
		return &FollowResult{Message: "フォローを解除しました。"}, nil
	}

	if err := s.mi.Follow(ctx, viewer.InstanceHost, token, userID); err != nil {
		if miauth.IsCode(err, miauth.CodeAlreadyFollowing) {
			return &FollowResult{Following: true, Message: "すでにフォローしています。"}, nil
		}
		return &FollowResult{Message: followErrMessage(err), FallbackURL: fallback}, nil
	}
	// 承認制(isLocked)の相手はフォロー申請として受け付けられる。
	locked := false
	if p, _, err := s.cached(ctx, targetID); err == nil && p != nil {
		locked = p.IsLocked
	}
	if locked {
		return &FollowResult{Pending: true, Message: "フォロー申請を送りました。"}, nil
	}
	return &FollowResult{Following: true, Message: "フォローしました。"}, nil
}

// pair loads both players, rejecting accounts with no Misskey linkage.
func (s *Service) pair(ctx context.Context, viewerID, targetID int64) (viewer, target *player.Player, err error) {
	if viewer, err = s.players.Get(ctx, viewerID); err != nil {
		return nil, nil, err
	}
	if target, err = s.players.Get(ctx, targetID); err != nil {
		return nil, nil, err
	}
	if viewer.InstanceHost == "" || target.InstanceHost == "" || target.RemoteUserID == "" {
		return nil, nil, fmt.Errorf("Misskeyアカウントが紐付いていません。")
	}
	return viewer, target, nil
}

// lookup asks the viewer's instance about the target, returning that instance's
// id for the account plus the viewer's relation to it. Residents of the same
// instance are looked up by id directly; others by username@host, which makes
// the instance resolve the account over ActivityPub if it does not know it yet.
//
// The resolved id is cached: it never changes, and having it lets a later call
// skip the acct lookup entirely.
func (s *Service) lookup(ctx context.Context, viewer, target *player.Player, targetID int64, token string) (*miauth.UserDetailed, error) {
	if viewer.InstanceHost == target.InstanceHost {
		return s.mi.ShowUser(ctx, viewer.InstanceHost, token, target.RemoteUserID)
	}
	if id, ok, err := s.cachedRemoteID(ctx, viewer.InstanceHost, targetID); err == nil && ok {
		if u, err := s.mi.ShowUser(ctx, viewer.InstanceHost, token, id); err == nil {
			return u, nil
		}
	}
	// username が要るので、プロフィールが未取得ならここで取る。
	prof, ok, err := s.cached(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if !ok {
		if prof, err = s.fetch(ctx, targetID); err != nil {
			return nil, err
		}
	}
	u, err := s.mi.ShowUserByAcct(ctx, viewer.InstanceHost, token, prof.Username, target.InstanceHost)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO misskey_resolved_users (viewer_host, target_player_id, resolved_user_id, resolved_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (viewer_host, target_player_id)
		 DO UPDATE SET resolved_user_id = EXCLUDED.resolved_user_id, resolved_at = now()`,
		viewer.InstanceHost, targetID, u.ID); err != nil {
		return nil, fmt.Errorf("cache resolved user: %w", err)
	}
	return u, nil
}

// cachedRemoteID returns a previously resolved id for the target on the viewer's
// instance.
func (s *Service) cachedRemoteID(ctx context.Context, viewerHost string, targetID int64) (string, bool, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT resolved_user_id FROM misskey_resolved_users
		  WHERE viewer_host = $1 AND target_player_id = $2`,
		viewerHost, targetID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read resolved user: %w", err)
	}
	return id, true, nil
}

// fallbackURL builds the viewer's remote-follow page for the target account.
func (s *Service) fallbackURL(ctx context.Context, viewer *player.Player, targetID int64) string {
	p, ok, err := s.cached(ctx, targetID)
	if err != nil || !ok {
		return ""
	}
	return miauth.RemoteFollowURL(viewer.InstanceHost, p.Acct)
}

// followErrMessage turns a Misskey API error into something a player can act on.
func followErrMessage(err error) string {
	switch {
	case miauth.IsCode(err, miauth.CodeRateLimitExceeded):
		return "フォローの回数制限に達しました。しばらく待つか、Misskey側で開いてフォローしてください。"
	case miauth.IsCode(err, miauth.CodePermissionDenied), miauth.IsCode(err, miauth.CodeAccessDenied):
		return "フォローの権限が許可されていません。ログインし直すと許可を求めます。"
	case miauth.IsCode(err, miauth.CodeAuthFailed):
		return "Misskeyの連携が切れています。ログインし直してください。"
	case miauth.IsCode(err, miauth.CodeBlocked), miauth.IsCode(err, miauth.CodeBlockee):
		return "この相手はフォローできません。"
	case miauth.IsCode(err, miauth.CodeNoSuchUser):
		return "相手のアカウントが見つかりませんでした。"
	}
	return err.Error()
}

// StaleAfter reports whether a cached profile is older than the TTL. Exposed for
// the UI to decide whether to show the fetch time.
func StaleAfter(fetchedAt time.Time) bool {
	return time.Since(fetchedAt) > cacheTTL
}
