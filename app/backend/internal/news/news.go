// Package news implements 街のニュース / 住民のイベント履歴 (役場). It replaces the
// legacy pair of ring-buffer files log_dir/mati_news.cgi (街ニュース100件) and
// log_dir/event_kanri.cgi (イベント記録150件) with a single append-only table:
// both hold the same shape of data (時刻・対象者・本文), so only the town_wide
// flag distinguishes "街全体に出す記事" from "本人の履歴だけに残る記事".
//
// Recording happens inside the caller's transaction (Record) so a failed action
// leaves no news behind; reads are pool-level.
package news

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// News kinds. These double as the display badge text in the 役場 feed.
const (
	KindMoveIn = "入居"     // 新規登録
	KindJob    = "就職"     // 転職
	KindHouse  = "家"      // 建築/建て替え/売却
	KindEvent  = "イベント"   // ランダムイベント
	KindPrize  = "当選"     // ロト6などの高額当選
)

// keep bounds the table length. The legacy files kept 100 (街) / 150 (個人)
// entries in total; we keep far more since this is a table, not a flat file.
const keep = 3000

// TownWideMoneyThreshold is the |money delta| at which a random event is
// promoted from the actor's private history to the town-wide feed. The legacy
// hard-coded which events made the news (地震 = 大損害, 運用 = 資産運用成功);
// keying on the amount instead generalises to admin-authored custom events.
const TownWideMoneyThreshold = 1_000_000

// Entry is one news article.
type Entry struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"`
	ActorID   *int64    `json:"actor_id"`
	ActorName string    `json:"actor_name"`
	Message   string    `json:"message"`
	Good      *bool     `json:"good"`
	At        time.Time `json:"at"`
}

// Service reads the news feed. Writes go through the package-level Record so
// callers can record inside their own transaction without holding a Service.
type Service struct {
	pool *pgxpool.Pool
}

// New returns a news service backed by pool.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Record appends one article inside tx and trims the table to the newest keep
// rows. actorID may be nil for articles with no owning player. good is nil for
// neutral articles (入居/家 など).
func Record(ctx context.Context, tx pgx.Tx, kind string, actorID *int64, actorName, message string, good *bool, townWide bool) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO town_news (kind, actor_id, actor_name, message, good, town_wide)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		kind, actorID, actorName, message, good, townWide); err != nil {
		return fmt.Errorf("insert news: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM town_news WHERE id NOT IN (SELECT id FROM town_news ORDER BY id DESC LIMIT $1)`,
		keep); err != nil {
		return fmt.Errorf("trim news: %w", err)
	}
	return nil
}

// RecordFor is Record for an article that always has an owning player.
func RecordFor(ctx context.Context, tx pgx.Tx, kind string, actorID int64, actorName, message string, good *bool, townWide bool) error {
	return Record(ctx, tx, kind, &actorID, actorName, message, good, townWide)
}

// ActorName reads a player's display name for use in a news message.
func ActorName(ctx context.Context, tx pgx.Tx, playerID int64) (string, error) {
	var name string
	if err := tx.QueryRow(ctx, `SELECT display_name FROM players WHERE id = $1`, playerID).Scan(&name); err != nil {
		return "", fmt.Errorf("read actor name: %w", err)
	}
	return name, nil
}

// ListTownWide returns the newest town-wide articles (役場「街のニュース」).
func (s *Service) ListTownWide(ctx context.Context, limit int) ([]Entry, error) {
	return s.query(ctx,
		`SELECT id, kind, actor_id, actor_name, message, good, created_at
		 FROM town_news WHERE town_wide ORDER BY id DESC LIMIT $1`, limit)
}

// ListByActor returns the newest articles about one resident, town-wide or not
// (役場「その住民の出来事」). The legacy 個人イベント was self-only; here any
// resident's history is readable from the 住民名鑑.
func (s *Service) ListByActor(ctx context.Context, playerID int64, limit int) ([]Entry, error) {
	return s.query(ctx,
		`SELECT id, kind, actor_id, actor_name, message, good, created_at
		 FROM town_news WHERE actor_id = $2 ORDER BY id DESC LIMIT $1`, limit, playerID)
}

func (s *Service) query(ctx context.Context, sql string, args ...any) ([]Entry, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query news: %w", err)
	}
	defer rows.Close()
	// 空でもJSON nullでなく[]を返す。
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Kind, &e.ActorID, &e.ActorName, &e.Message, &e.Good, &e.At); err != nil {
			return nil, fmt.Errorf("scan news: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
