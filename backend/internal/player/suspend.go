package player

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// MaxSuspendReason caps the reason shown to the suspended resident.
const MaxSuspendReason = 200

// Suspension is a resident's login block.
type Suspension struct {
	Suspended bool       // 凍結が設定されている(期限切れを含む)
	Forever   bool       // 無期限
	Until     *time.Time // いつまで。無期限なら nil
	Reason    string
}

// Active reports whether the block is in force right now.
func (s Suspension) Active() bool {
	if !s.Suspended {
		return false
	}
	if s.Forever {
		return true
	}
	return s.Until != nil && time.Now().Before(*s.Until)
}

// SuspensionSQL reads the two columns as a Suspension. 無期限は 'infinity' で
// 持っているが、この値は time.Time に読めないのでSQLの側で畳んでおく
// (bool + NULL に分ける)。読む場所が増えても式が散らないよう定数にする。
const SuspensionSQL = `
	p.suspended_until IS NOT NULL,
	COALESCE(p.suspended_until = 'infinity'::timestamptz, false),
	CASE WHEN p.suspended_until = 'infinity'::timestamptz THEN NULL ELSE p.suspended_until END,
	p.suspend_reason`

// Message is what the resident is told at the login screen.
func (s Suspension) Message() string {
	if !s.Active() {
		return ""
	}
	when := "無期限で"
	if !s.Forever {
		when = s.Until.Local().Format("2006年1月2日 15:04") + "まで"
	}
	msg := "この街の利用を" + when + "停止しています。"
	if s.Reason != "" {
		msg += "\n理由: " + s.Reason
	}
	return msg
}

// GetSuspension reads a resident's login block.
func (s *Service) GetSuspension(ctx context.Context, id int64) (Suspension, error) {
	var out Suspension
	err := s.pool.QueryRow(ctx,
		`SELECT `+SuspensionSQL+` FROM players p WHERE p.id = $1`, id).
		Scan(&out.Suspended, &out.Forever, &out.Until, &out.Reason)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Suspension{}, ErrNotFound
		}
		return Suspension{}, fmt.Errorf("read suspension: %w", err)
	}
	return out, nil
}

// AdminSuspend blocks a resident from logging in. days <= 0 means forever.
//
// 開いたままの画面を残さないよう、その人のセッションも消す。凍結してもcookieが
// 生きていると、次に何か操作するまで止まったことに気づけない。
func (s *Service) AdminSuspend(ctx context.Context, id int64, days int, reason string) error {
	reason = strings.TrimSpace(reason)
	if r := []rune(reason); len(r) > MaxSuspendReason {
		reason = string(r[:MaxSuspendReason])
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var isAdmin bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM player_roles WHERE player_id = $1 AND role = 'admin')`,
			id).Scan(&isAdmin); err != nil {
			return fmt.Errorf("check role: %w", err)
		}
		if isAdmin {
			// 自分や他の管理者を締め出すと、解除する手段ごと失う。
			return &ErrValidation{Message: "管理者は凍結できません。先に管理者権限を外してください。"}
		}
		var until any = "infinity"
		if days > 0 {
			until = time.Now().Add(time.Duration(days) * 24 * time.Hour)
		}
		tag, err := tx.Exec(ctx,
			`UPDATE players SET suspended_until = $2, suspend_reason = $3
			 WHERE id = $1 AND deleted_at IS NULL`, id, until, reason)
		if err != nil {
			return fmt.Errorf("suspend: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE player_id = $1`, id); err != nil {
			return fmt.Errorf("drop sessions: %w", err)
		}
		return nil
	})
}

// AdminUnsuspend lifts the block.
func (s *Service) AdminUnsuspend(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE players SET suspended_until = NULL, suspend_reason = '' WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("unsuspend: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrValidation wraps a user-facing failure of an admin operation.
type ErrValidation struct{ Message string }

func (e *ErrValidation) Error() string { return e.Message }

// AdminIDs lists the players holding the admin role. 目安箱の新着など、運営に
// 知らせたいときの宛先。
func (s *Service) AdminIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT r.player_id FROM player_roles r
		 JOIN players p ON p.id = r.player_id AND p.deleted_at IS NULL
		 WHERE r.role = 'admin' ORDER BY r.player_id`)
	if err != nil {
		return nil, fmt.Errorf("admin ids: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan admin id: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
