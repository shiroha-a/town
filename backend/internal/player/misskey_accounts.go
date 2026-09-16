// Misskeyアカウントの複数連携。仕様: .tmp/design_misskey_accounts.md
//
// 住民の識別キーを players(instance_host, remote_user_id) に固定していると、
// 連携先のインスタンスがサービス終了した住民が二度とログインできない。
// ログインの識別は player_misskey_accounts に持ち、players 側の3列は
// 「代表アカウントの写し」として残す(既存の参照を書き換えずに済ませるため)。

package player

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrAccountTaken means the Misskey account is linked to a different resident.
	ErrAccountTaken = errors.New("account already linked to another player")
	// ErrAccountNotFound means the player has no such linked account.
	ErrAccountNotFound = errors.New("linked account not found")
	// ErrLastAccount guards the only remaining way to log in.
	ErrLastAccount = errors.New("cannot unlink the last account")
	// ErrPrimaryAccount means the account is the one shown publicly; switch first.
	ErrPrimaryAccount = errors.New("cannot unlink the primary account")
)

// MisskeyAccount is one linked Misskey account.
type MisskeyAccount struct {
	Host         string `json:"host"`
	RemoteUserID string `json:"remote_user_id"`
	Username     string `json:"username"`
	Acct         string `json:"acct"`
	// Primary marks the account shown on the profile page and used as the origin
	// for follows. 代表は players 側の値と一致する行(専用の列は持たない)。
	Primary bool `json:"primary"`
	// HasToken is false when we hold no usable access token (鍵が未設定か、
	// 連携より前から居た住民)。フォローが使えるかの判断に出す。
	HasToken bool      `json:"has_token"`
	LinkedAt time.Time `json:"linked_at"`
}

// ListMisskeyAccounts returns the player's linked accounts, primary first.
func (s *Service) ListMisskeyAccounts(ctx context.Context, playerID int64) ([]MisskeyAccount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT a.host, a.remote_user_id, a.username, a.token_enc IS NOT NULL,
		        (a.host = p.instance_host AND a.remote_user_id = p.remote_user_id) AS is_primary,
		        a.linked_at
		   FROM player_misskey_accounts a
		   JOIN players p ON p.id = a.player_id
		  WHERE a.player_id = $1
		  ORDER BY is_primary DESC, a.linked_at`, playerID)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()
	out := []MisskeyAccount{}
	for rows.Next() {
		var a MisskeyAccount
		if err := rows.Scan(&a.Host, &a.RemoteUserID, &a.Username, &a.HasToken,
			&a.Primary, &a.LinkedAt); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		a.Acct = "@" + a.Username + "@" + a.Host
		out = append(out, a)
	}
	return out, rows.Err()
}

// FindByMisskeyAccount returns the player id linked to the account, or 0.
func (s *Service) FindByMisskeyAccount(ctx context.Context, host, remoteUserID string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`SELECT player_id FROM player_misskey_accounts WHERE host = $1 AND remote_user_id = $2`,
		host, remoteUserID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find account: %w", err)
	}
	return id, nil
}

// LinkMisskeyAccount attaches another Misskey account to the player. Linking an
// account the player already holds only refreshes its token.
//
// 相手インスタンスへは一切問い合わせない: 代表のインスタンスが既に終了していても
// 別サーバーを足せることが、この機能の存在理由そのものなので。
func (s *Service) LinkMisskeyAccount(ctx context.Context, playerID int64, host, remoteUserID, username, token string) error {
	owner, err := s.FindByMisskeyAccount(ctx, host, remoteUserID)
	if err != nil {
		return err
	}
	if owner != 0 && owner != playerID {
		return ErrAccountTaken
	}
	enc, err := s.sealToken(token)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO player_misskey_accounts (host, remote_user_id, player_id, username, token_enc)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (host, remote_user_id) DO UPDATE
		    SET username = EXCLUDED.username,
		        -- 鍵が未設定でトークンを持てない構成では、既存の値を消さない。
		        token_enc = COALESCE(EXCLUDED.token_enc, player_misskey_accounts.token_enc)`,
		host, remoteUserID, playerID, username, enc); err != nil {
		return fmt.Errorf("link account: %w", err)
	}
	// 代表アカウントを足し直したときは、players側の写しも新しいトークンに揃える。
	if _, err := s.pool.Exec(ctx,
		`UPDATE players SET misskey_token_enc = COALESCE($4, misskey_token_enc)
		  WHERE id = $1 AND instance_host = $2 AND remote_user_id = $3`,
		playerID, host, remoteUserID, enc); err != nil {
		return fmt.Errorf("sync primary token: %w", err)
	}
	return nil
}

// SetPrimaryMisskeyAccount makes the account the one shown publicly and used as
// the origin for follows. It reports whether anything changed.
//
// プロフィールのキャッシュ破棄は呼び出し側(httpapi)が行う。取り直しは相手
// インスタンスへの問い合わせを伴い、この層の責務から外れるため。
func (s *Service) SetPrimaryMisskeyAccount(ctx context.Context, playerID int64, host, remoteUserID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var enc []byte
		err := tx.QueryRow(ctx,
			`SELECT token_enc FROM player_misskey_accounts
			  WHERE player_id = $1 AND host = $2 AND remote_user_id = $3`,
			playerID, host, remoteUserID).Scan(&enc)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		if err != nil {
			return fmt.Errorf("read account: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE players SET instance_host = $2, remote_user_id = $3, misskey_token_enc = $4
			  WHERE id = $1`, playerID, host, remoteUserID, enc); err != nil {
			return fmt.Errorf("set primary: %w", err)
		}
		// 取得元が変わるのでプロフィールのキャッシュは捨てる。残すと、前の
		// アカウントの自己紹介やアイコンが新しい代表のものとして出続ける。
		if _, err := tx.Exec(ctx,
			`DELETE FROM misskey_profiles WHERE player_id = $1`, playerID); err != nil {
			return fmt.Errorf("drop profile cache: %w", err)
		}
		// 他インスタンスが解決済みのIDは前のアカウントを指している。消さないと
		// フォローが別人に飛ぶ。
		if _, err := tx.Exec(ctx,
			`DELETE FROM misskey_resolved_users WHERE target_player_id = $1`, playerID); err != nil {
			return fmt.Errorf("drop resolved cache: %w", err)
		}
		return nil
	})
}

// UnlinkMisskeyAccount removes a linked account. The last one and the primary
// one are protected: losing either would leave the player unable to log in or
// without a public identity.
func (s *Service) UnlinkMisskeyAccount(ctx context.Context, playerID int64, host, remoteUserID string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var isPrimary bool
		err := tx.QueryRow(ctx,
			`SELECT (a.host = p.instance_host AND a.remote_user_id = p.remote_user_id)
			   FROM player_misskey_accounts a
			   JOIN players p ON p.id = a.player_id
			  WHERE a.player_id = $1 AND a.host = $2 AND a.remote_user_id = $3`,
			playerID, host, remoteUserID).Scan(&isPrimary)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAccountNotFound
		}
		if err != nil {
			return fmt.Errorf("read account: %w", err)
		}
		var count int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM player_misskey_accounts WHERE player_id = $1`, playerID).Scan(&count); err != nil {
			return fmt.Errorf("count accounts: %w", err)
		}
		if count <= 1 {
			return ErrLastAccount
		}
		if isPrimary {
			return ErrPrimaryAccount
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM player_misskey_accounts
			  WHERE player_id = $1 AND host = $2 AND remote_user_id = $3`,
			playerID, host, remoteUserID); err != nil {
			return fmt.Errorf("unlink account: %w", err)
		}
		return nil
	})
}

// linkPrimaryAccount records the player's own identity as a linked account, so
// that every resident has at least one way to log in listed in one place.
// 既に別の住民のものとして入っていれば何もしない(横取りしない)。
func linkPrimaryAccount(ctx context.Context, tx pgx.Tx, playerID int64, host, remoteUserID string) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO player_misskey_accounts (host, remote_user_id, player_id)
		 VALUES ($1, $2, $3) ON CONFLICT (host, remote_user_id) DO NOTHING`,
		host, remoteUserID, playerID); err != nil {
		return fmt.Errorf("link primary account: %w", err)
	}
	return nil
}

// sealToken encrypts a token for storage, returning nil when no key is
// configured (the token is then not stored at all rather than in the clear).
func (s *Service) sealToken(token string) ([]byte, error) {
	if s.tokenCipher == nil || token == "" {
		return nil, nil
	}
	enc, err := s.tokenCipher.Seal(token)
	if err != nil {
		return nil, fmt.Errorf("seal token: %w", err)
	}
	return enc, nil
}
