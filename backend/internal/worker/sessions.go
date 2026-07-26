package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PurgeSessions removes expired login sessions and stale pending MiAuth
// sessions. Expired sessions are already rejected at lookup time; this keeps
// the tables from growing without bound.
func PurgeSessions(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`); err != nil {
		return fmt.Errorf("purge sessions: %w", err)
	}
	// 承認されないまま放置された保留セッション(1時間で無効)。
	if _, err := tx.Exec(ctx,
		`DELETE FROM auth_sessions WHERE created_at < now() - interval '1 hour'`); err != nil {
		return fmt.Errorf("purge auth_sessions: %w", err)
	}
	return nil
}
