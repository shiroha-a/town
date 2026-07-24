// Package greeting implements あいさつ, the town chat / one-line board. Posting a
// greeting can earn random money (with a jackpot and a rock-paper-scissors
// multiplier), 宣伝 posts cost money, and NG words are fined — that game logic
// lives in the action service; this package holds the shared data, validation
// constants and read helpers.
package greeting

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// MaxBodyRunes is the greeting length cap (legacy ~60 zenkaku).
	MaxBodyRunes = 60
	// AdRevCost is the yen cost of a 宣伝 (advertisement) post.
	AdRevCost = 20000
	// NGFine is the yen fine for including an NG word.
	NGFine = 30000
	// keep bounds the stored greeting count.
	keep = 100
	// CatAd / CatAdmin are the special categories.
	CatAd    = "宣伝"
	CatAdmin = "管理人"
)

// Categories are the selectable normal greeting kinds (legacy @aisatu_keyword).
var Categories = []string{"あいさつ", "雑談", "今日の出来事", "今の気分", "なんとなく", "お話ししよう"}

// NGWords trigger the fine and are masked in the stored body.
var NGWords = []string{"ぼけ", "あーほ"}

// Greeting is one board post.
type Greeting struct {
	ID       int64  `json:"id"`
	UserID   *int64 `json:"user_id"`
	UserName string `json:"user_name"`
	Category string `json:"category"`
	Body     string `json:"body"`
	Color    string `json:"color"`
	Janken   string `json:"janken"`
	PostedAt string `json:"posted_at"`
}

// Keep is the retained greeting count (exported for the writer's trim).
const Keep = keep

// Service reads the greeting board.
type Service struct {
	pool *pgxpool.Pool
}

// New builds the read service.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// List returns the most recent greetings, newest first.
func (s *Service) List(ctx context.Context, limit int) ([]Greeting, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, user_name, category, body, color, COALESCE(janken, ''), posted_at
		 FROM greetings ORDER BY posted_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list greetings: %w", err)
	}
	defer rows.Close()
	out := []Greeting{}
	for rows.Next() {
		var (
			g      Greeting
			posted time.Time
		)
		if err := rows.Scan(&g.ID, &g.UserID, &g.UserName, &g.Category, &g.Body, &g.Color, &g.Janken, &posted); err != nil {
			return nil, err
		}
		// タイムゾーンはクライアント側で表示時に解決する(RFC3339で返す)。
		g.PostedAt = posted.Format(time.RFC3339)
		out = append(out, g)
	}
	return out, rows.Err()
}

// Delete removes one greeting (admin moderation).
func (s *Service) Delete(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM greetings WHERE id = $1`, id)
	return err
}
