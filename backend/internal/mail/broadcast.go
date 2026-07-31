package mail

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// MaxBroadcastBody caps an announcement. 受信箱の1通ぶんに収まる長さ。
const MaxBroadcastBody = 2000

// Broadcast delivers one message to every resident, for announcements from the
// operator.
//
// お知らせ専用の仕組みは作らず、既にあるメールに乗せる。受信箱・端末への通知
// (workerが新着メールを見て送る)・保存の扱いがそのまま使えるため。
//
// 日次の送信上限(DailySendLimit)は通さない。運営の告知が1日30通で止まると
// 意味が無い。宛先は退会していない非ゲスト全員。ゲストは1時間で消えるので送らない。
func (s *Service) Broadcast(ctx context.Context, senderID int64, body string) (int, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, &ErrValidation{Message: "本文が入力されていません。"}
	}
	if len([]rune(body)) > MaxBroadcastBody {
		return 0, &ErrValidation{Message: fmt.Sprintf("本文は%d文字までです。", MaxBroadcastBody)}
	}
	var sent int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var senderName string
		if err := tx.QueryRow(ctx,
			`SELECT display_name FROM players WHERE id = $1 AND deleted_at IS NULL`,
			senderID).Scan(&senderName); err != nil {
			return fmt.Errorf("sender: %w", err)
		}
		rows, err := tx.Query(ctx,
			`SELECT id FROM players
			 WHERE deleted_at IS NULL AND NOT is_guest AND id <> $1
			 ORDER BY id`, senderID)
		if err != nil {
			return fmt.Errorf("list recipients: %w", err)
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scan recipient: %w", err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("list recipients: %w", err)
		}
		for _, id := range ids {
			if _, err := tx.Exec(ctx,
				`INSERT INTO messages (owner_id, direction, counterpart_id, counterpart_name, body)
				 VALUES ($1, 'received', $2, $3, $4)`, id, senderID, senderName, body); err != nil {
				return fmt.Errorf("insert received: %w", err)
			}
			if err := trim(ctx, tx, id); err != nil {
				return err
			}
		}
		// 送信者側は控えを1通だけ残す。宛先ぶんの送信済みを並べると受信箱が
		// 自分の告知で埋まる。相手は特定の1人ではないので counterpart_id は空。
		if _, err := tx.Exec(ctx,
			`INSERT INTO messages (owner_id, direction, counterpart_id, counterpart_name, body)
			 VALUES ($1, 'sent', NULL, $2, $3)`,
			senderID, fmt.Sprintf("全員へ一斉送信(%d人)", len(ids)), body); err != nil {
			return fmt.Errorf("insert sent: %w", err)
		}
		if err := trim(ctx, tx, senderID); err != nil {
			return err
		}
		sent = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return sent, nil
}

// SendNotice delivers a system notice without touching the sender's daily quota
// and without leaving a copy in their sent box.
//
// 目安箱の知らせのように、本人が書いたつもりのないメールに使う。住民の投稿が
// 管理者への通知になるので、それで本人の1日30通を削るのはおかしい。
func (s *Service) SendNotice(ctx context.Context, senderID, recipientID int64, body string) error {
	body = strings.TrimSpace(body)
	if body == "" || senderID == recipientID {
		return nil
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var senderName string
		if err := tx.QueryRow(ctx,
			`SELECT display_name FROM players WHERE id = $1 AND deleted_at IS NULL`,
			senderID).Scan(&senderName); err != nil {
			return fmt.Errorf("sender: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO messages (owner_id, direction, counterpart_id, counterpart_name, body)
			 VALUES ($1, 'received', $2, $3, $4)`,
			recipientID, senderID, senderName, body); err != nil {
			return fmt.Errorf("insert notice: %w", err)
		}
		return trim(ctx, tx, recipientID)
	})
}
