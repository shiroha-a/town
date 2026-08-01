// Package mail implements player-to-player messaging (メール): a shared inbox and
// sent box per player, with delete, a save/protect flag, a per-owner FIFO cap,
// a daily send limit and unread-count notification. Recipients are addressed by
// id (not name) to avoid the legacy's same-name ambiguity, and bodies are stored
// as plain text and escaped on display.
package mail

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/gametime"
)

const (
	// SaveLimit is the per-owner mailbox cap; older non-saved mail is trimmed.
	SaveLimit = 50
	// DailySendLimit is the max messages one player may send per game day.
	DailySendLimit = 30
	// MaxBody caps one letter between residents. 日数あたりの通数だけ絞っても
	// 1通の長さが青天井だと、受信箱を重くする手が残る。運営からのお知らせ
	// (MaxBroadcastBody)より短くしているのは、住民の手紙にそこまでの長さが
	// 要らないため。
	MaxBody = 1000
)

// ErrValidation wraps a user-facing validation failure.
type ErrValidation struct{ Message string }

func (e *ErrValidation) Error() string { return e.Message }

// Message is one mailbox entry from the owner's perspective.
type Message struct {
	ID              int64     `json:"id"`
	Direction       string    `json:"direction"` // received | sent
	CounterpartID   *int64    `json:"counterpart_id"`
	CounterpartName string    `json:"counterpart_name"`
	Body            string    `json:"body"`
	SentAt          time.Time `json:"sent_at"`
	Saved           bool      `json:"saved"`
	Unread          bool      `json:"unread"`
	GiftItemName    string    `json:"gift_item_name"` // 添付された贈り物(無ければ空)
}

// Contact is an address-book entry (a recent counterpart).
type Contact struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Mailbox bundles a player's mail view.
type Mailbox struct {
	Received    []Message `json:"received"`
	Sent        []Message `json:"sent"`
	AddressBook []Contact `json:"address_book"`
	Unread      int       `json:"unread"`
}

// Service provides mail reads and writes.
type Service struct {
	pool            *pgxpool.Pool
	loc             *time.Location
	dayBoundaryHour int
}

// New builds the service. loc/dayBoundaryHour define the game-day used for the
// daily send limit.
func New(pool *pgxpool.Pool, loc *time.Location, dayBoundaryHour int) *Service {
	return &Service{pool: pool, loc: loc, dayBoundaryHour: dayBoundaryHour}
}

// dayStart returns the wall-clock start of the current game day.
func (s *Service) dayStart(now time.Time) time.Time {
	return gametime.Date(now, s.loc, s.dayBoundaryHour).Add(time.Duration(s.dayBoundaryHour) * time.Hour)
}

// GetMailbox returns the player's received/sent lists, address book and unread
// count (relative to the last check time — it does not update it).
func (s *Service) GetMailbox(ctx context.Context, playerID int64) (Mailbox, error) {
	var mb Mailbox
	lastChecked, err := s.lastChecked(ctx, playerID)
	if err != nil {
		return mb, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, direction, counterpart_id, counterpart_name, body, saved, sent_at, gift_item_name
		 FROM messages WHERE owner_id = $1 ORDER BY sent_at DESC, id DESC`, playerID)
	if err != nil {
		return mb, fmt.Errorf("query mailbox: %w", err)
	}
	defer rows.Close()
	mb.Received, mb.Sent = []Message{}, []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Direction, &m.CounterpartID, &m.CounterpartName, &m.Body, &m.Saved, &m.SentAt, &m.GiftItemName); err != nil {
			return mb, err
		}
		if m.Direction == "received" {
			m.Unread = m.SentAt.After(lastChecked)
			if m.Unread {
				mb.Unread++
			}
			mb.Received = append(mb.Received, m)
		} else {
			mb.Sent = append(mb.Sent, m)
		}
	}
	if err := rows.Err(); err != nil {
		return mb, err
	}
	mb.AddressBook, err = s.addressBook(ctx, playerID)
	return mb, err
}

func (s *Service) lastChecked(ctx context.Context, playerID int64) (time.Time, error) {
	var t time.Time
	err := s.pool.QueryRow(ctx, `SELECT last_checked_at FROM mail_check WHERE player_id = $1`, playerID).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Unix(0, 0), nil // 未チェック: すべて新着扱い
	}
	if err != nil {
		return t, fmt.Errorf("last checked: %w", err)
	}
	return t, nil
}

func (s *Service) addressBook(ctx context.Context, playerID int64) ([]Contact, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT counterpart_id, counterpart_name FROM messages
		 WHERE owner_id = $1 AND counterpart_id IS NOT NULL
		 GROUP BY counterpart_id, counterpart_name
		 ORDER BY MAX(sent_at) DESC LIMIT 30`, playerID)
	if err != nil {
		return nil, fmt.Errorf("address book: %w", err)
	}
	defer rows.Close()
	out := []Contact{}
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UnreadCount returns how many received messages arrived since the last check
// (used by the town-screen notification without marking as read).
func (s *Service) UnreadCount(ctx context.Context, playerID int64) (int, error) {
	lastChecked, err := s.lastChecked(ctx, playerID)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages
		 WHERE owner_id = $1 AND direction = 'received' AND sent_at > $2`, playerID, lastChecked).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("unread count: %w", err)
	}
	return n, nil
}

// MarkChecked records that the player has opened their inbox now.
func (s *Service) MarkChecked(ctx context.Context, playerID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO mail_check (player_id, last_checked_at) VALUES ($1, now())
		 ON CONFLICT (player_id) DO UPDATE SET last_checked_at = now()`, playerID)
	return err
}

// Send delivers a plain-text message to a recipient, storing a received copy in
// the recipient's box and a sent copy in the sender's, trimming both to
// SaveLimit. Enforces non-empty body, no self-send, and the daily send limit.
// giftGranter hands one unit of a held gift to the recipient. Injected by the
// caller so mail does not need to know about items/inventory.
type giftGranter func(ctx context.Context, tx pgx.Tx, senderID, recipientID, giftID int64) (string, error)

// Send delivers a message. giftID > 0 attaches one unit of the sender's gift,
// which grantGift moves into the recipient's inventory (レガシー command.pl の
// gift_souhu)。grantGift may be nil when no gift support is wired up.
func (s *Service) Send(ctx context.Context, senderID, recipientID int64, body string, giftID int64, grantGift giftGranter) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return &ErrValidation{Message: "メッセージが入力されていません。"}
	}
	if len([]rune(body)) > MaxBody {
		return &ErrValidation{Message: fmt.Sprintf("本文は%d文字までです。", MaxBody)}
	}
	if senderID == recipientID {
		return &ErrValidation{Message: "自分に送っても意味がありません。"}
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var senderName, recipientName string
		if err := tx.QueryRow(ctx,
			`SELECT display_name FROM players WHERE id = $1 AND deleted_at IS NULL`, senderID).Scan(&senderName); err != nil {
			return fmt.Errorf("sender: %w", err)
		}
		if err := tx.QueryRow(ctx,
			`SELECT display_name FROM players WHERE id = $1 AND deleted_at IS NULL`, recipientID).Scan(&recipientName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &ErrValidation{Message: "相手が見つかりません。"}
			}
			return fmt.Errorf("recipient: %w", err)
		}
		var sent int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM messages
			 WHERE owner_id = $1 AND direction = 'sent' AND sent_at >= $2`,
			senderID, s.dayStart(time.Now())).Scan(&sent); err != nil {
			return fmt.Errorf("daily count: %w", err)
		}
		if sent >= DailySendLimit {
			return &ErrValidation{Message: fmt.Sprintf("本日の送信数(%d通)を超えました。", DailySendLimit)}
		}
		// 贈り物の添付。相手の持ち物へ移してから、両方の行に品名を残す。
		var giftName string
		if giftID > 0 {
			if grantGift == nil {
				return &ErrValidation{Message: "贈り物は送れません。"}
			}
			var err error
			if giftName, err = grantGift(ctx, tx, senderID, recipientID, giftID); err != nil {
				return err
			}
		}
		// 受信側と送信側に同時刻で複製保存。
		if _, err := tx.Exec(ctx,
			`INSERT INTO messages (owner_id, direction, counterpart_id, counterpart_name, body, gift_item_name)
			 VALUES ($1, 'received', $2, $3, $4, $5)`, recipientID, senderID, senderName, body, giftName); err != nil {
			return fmt.Errorf("insert received: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO messages (owner_id, direction, counterpart_id, counterpart_name, body, gift_item_name)
			 VALUES ($1, 'sent', $2, $3, $4, $5)`, senderID, recipientID, recipientName, body, giftName); err != nil {
			return fmt.Errorf("insert sent: %w", err)
		}
		if err := trim(ctx, tx, recipientID); err != nil {
			return err
		}
		return trim(ctx, tx, senderID)
	})
}

// trim enforces the per-owner SaveLimit, keeping saved and most-recent messages.
func trim(ctx context.Context, tx pgx.Tx, ownerID int64) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM messages WHERE owner_id = $1 AND id NOT IN (
		   SELECT id FROM messages WHERE owner_id = $1
		   ORDER BY saved DESC, sent_at DESC, id DESC LIMIT $2)`, ownerID, SaveLimit)
	if err != nil {
		return fmt.Errorf("trim: %w", err)
	}
	return nil
}

// Delete removes one of the player's messages.
func (s *Service) Delete(ctx context.Context, playerID, msgID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM messages WHERE id = $1 AND owner_id = $2`, msgID, playerID)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &ErrValidation{Message: "メッセージが見つかりません。"}
	}
	return nil
}

// SetSaved toggles the save/protect flag on one of the player's messages.
func (s *Service) SetSaved(ctx context.Context, playerID, msgID int64, saved bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE messages SET saved = $3 WHERE id = $1 AND owner_id = $2`, msgID, playerID, saved)
	if err != nil {
		return fmt.Errorf("set saved: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &ErrValidation{Message: "メッセージが見つかりません。"}
	}
	return nil
}
