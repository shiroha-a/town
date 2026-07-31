// Package moderation gives the operator one place to read and remove what
// residents wrote, across every board in the town.
//
// 種類ごとに管理の欄を作ると、書き込める場所が増えるたびに管理画面も増える。
// 荒らしへの対応は「その人の書き込みを全部見て消す」なので、出どころを横断した
// 1本の時系列にして、投稿者で絞れることを要点にした。
package moderation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Source is where a post came from.
type Source string

const (
	SourceGreeting   Source = "greeting"    // あいさつ(街トップのチャット)
	SourceHouseBBS   Source = "house_bbs"   // 家の掲示板
	SourceCompanyBBS Source = "company_bbs" // 会社の掲示板
	SourceFeedback   Source = "feedback"    // 目安箱の投稿
	SourceFeedbackC  Source = "feedback_c"  // 目安箱のコメント
)

// SourceLabels are the Japanese names shown in the admin screen.
var SourceLabels = map[Source]string{
	SourceGreeting:   "あいさつ",
	SourceHouseBBS:   "家の掲示板",
	SourceCompanyBBS: "会社の掲示板",
	SourceFeedback:   "目安箱",
	SourceFeedbackC:  "目安箱(返信)",
}

// ErrUnknownSource means the caller named a board that does not exist.
var ErrUnknownSource = errors.New("unknown post source")

// Post is one writing, in the shape shared by every board.
type Post struct {
	Source    Source    `json:"source"`
	ID        int64     `json:"id"`
	PlayerID  *int64    `json:"player_id"` // 退会で消えるとnull
	Author    string    `json:"author"`
	Title     string    `json:"title"` // 無い板では空
	Body      string    `json:"body"`
	Where     string    `json:"where"` // 「家#12」のような置き場所
	CreatedAt time.Time `json:"created_at"`
}

// Service reads and removes posts.
type Service struct{ pool *pgxpool.Pool }

// New builds the service.
func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// selectSQL is one board's rows in the shared shape. $1=出どころの絞り込み
// (空なら全部)、$2=投稿者の絞り込み(0なら全員)。
//
// UNION ALL で束ねてから並べ替える。板ごとに列名も持ち物も違うので、ここで
// 名前を揃えてしまうのが一番読みやすい。
const selectSQL = `
SELECT * FROM (
  SELECT 'greeting' AS source, g.id, g.user_id AS player_id, g.user_name AS author,
         '' AS title, g.body, '' AS place, g.posted_at AS created_at
    FROM greetings g
  UNION ALL
  SELECT 'house_bbs', b.id, b.author_id, b.author_name,
         b.title, b.body, '家#' || b.house_id::text, b.created_at
    FROM house_bbs b
  UNION ALL
  SELECT 'company_bbs', c.id, c.author_id, c.author_name,
         '', c.body, '会社#' || c.house_id::text || ' / ' || c.board, c.created_at
    FROM company_bbs c
  UNION ALL
  SELECT 'feedback', f.id, f.author_id, f.author_name,
         f.title, f.body, f.kind, f.created_at
    FROM feedback_posts f
  UNION ALL
  SELECT 'feedback_c', fc.id, fc.author_id, fc.author_name,
         '', fc.body, '投稿#' || fc.post_id::text, fc.created_at
    FROM feedback_comments fc
) AS p
WHERE ($1 = '' OR p.source = $1)
  AND ($2 = 0 OR p.player_id = $2)
ORDER BY p.created_at DESC, p.id DESC
LIMIT $3`

// List returns recent posts, newest first. source が空なら全部、playerID が 0 なら
// 全員ぶん。
func (s *Service) List(ctx context.Context, source string, playerID int64, limit int) ([]Post, error) {
	if source != "" {
		if _, ok := SourceLabels[Source(source)]; !ok {
			return nil, ErrUnknownSource
		}
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, selectSQL, source, playerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list posts: %w", err)
	}
	defer rows.Close()
	out := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.Source, &p.ID, &p.PlayerID, &p.Author,
			&p.Title, &p.Body, &p.Where, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// deleteSQL maps a source onto the table it lives in. 表名を組み立てずに
// 引き当てるのは、外から来た文字列をSQLに混ぜないため。
var deleteSQL = map[Source]string{
	SourceGreeting:   `DELETE FROM greetings WHERE id = $1`,
	SourceHouseBBS:   `DELETE FROM house_bbs WHERE id = $1`,
	SourceCompanyBBS: `DELETE FROM company_bbs WHERE id = $1`,
	SourceFeedback:   `DELETE FROM feedback_posts WHERE id = $1`,
	SourceFeedbackC:  `DELETE FROM feedback_comments WHERE id = $1`,
}

// Delete removes one post. 消えていた場合も成功として扱う(二重に押しても
// エラーにしない)。
func (s *Service) Delete(ctx context.Context, source string, id int64) error {
	q, ok := deleteSQL[Source(source)]
	if !ok {
		return ErrUnknownSource
	}
	if _, err := s.pool.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	return nil
}
