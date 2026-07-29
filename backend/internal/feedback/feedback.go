// Package feedback implements 目安箱: the in-game place to report a bug or ask
// for a feature. It carries the parts of a GitHub issue that are worth having at
// this scale — a kind, a status only staff can move, comments, and one 賛同 per
// player — and nothing else.
package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kinds are what a post is about.
var Kinds = []string{"bug", "request", "question"}

// Statuses are the lifecycle of a post; only an admin may move it.
var Statuses = []string{"open", "triage", "doing", "done", "wontfix"}

// KindLabels / StatusLabels are the Japanese names shown in the UI. The server
// keeps them so the labels cannot drift between screens.
var KindLabels = map[string]string{"bug": "不具合", "request": "要望", "question": "質問"}
var StatusLabels = map[string]string{
	"open": "受付", "triage": "検討中", "doing": "対応中", "done": "対応済み", "wontfix": "見送り",
}

func valid(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Limits on what a player may write. Long enough to describe a bug, short enough
// that one post cannot flood the list.
const (
	MaxTitle   = 40
	MaxBody    = 1000
	MaxComment = 500
	// PostInterval throttles new posts per player (連投よけ)。
	PostInterval = 3 * time.Minute
)

// ErrValidation is bad input from a player (mapped to 422).
type ErrValidation struct{ Message string }

func (e *ErrValidation) Error() string { return e.Message }

// ErrForbidden is an operation the player may not perform (mapped to 403).
type ErrForbidden struct{ Message string }

func (e *ErrForbidden) Error() string { return e.Message }

// ErrNotFound means the post is gone.
var ErrNotFound = errors.New("post not found")

// Post is one entry of the list.
type Post struct {
	ID          int64  `json:"id"`
	AuthorID    *int64 `json:"author_id"`
	AuthorName  string `json:"author_name"`
	Kind        string `json:"kind"`
	KindLabel   string `json:"kind_label"`
	Status      string `json:"status"`
	StatusLabel string `json:"status_label"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Votes       int    `json:"votes"`
	// Voted reports whether the viewer has already agreed with this post.
	Voted     bool      `json:"voted"`
	Comments  int       `json:"comments"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Comment is one reply.
type Comment struct {
	ID         int64     `json:"id"`
	AuthorID   *int64    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	IsStaff    bool      `json:"is_staff"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
}

// Detail is a post with its comments.
type Detail struct {
	Post     Post      `json:"post"`
	Comments []Comment `json:"comments"`
}

// Service reads and writes the 目安箱.
type Service struct{ pool *pgxpool.Pool }

// New builds the service.
func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ListOptions filters and orders the list.
type ListOptions struct {
	// Kind / Status are empty for "all".
	Kind   string
	Status string
	// Sort is "new" (default) or "votes".
	Sort string
	// Viewer is the logged-in player (0 when not logged in) — used for Voted.
	Viewer int64
}

const postColumns = `p.id, p.author_id, p.author_name, p.kind, p.status, p.title, p.body,
	(SELECT COUNT(*) FROM feedback_votes v WHERE v.post_id = p.id)::int,
	(SELECT COUNT(*) FROM feedback_comments c WHERE c.post_id = p.id)::int,
	EXISTS (SELECT 1 FROM feedback_votes v WHERE v.post_id = p.id AND v.player_id = $1),
	p.created_at, p.updated_at`

func scanPost(row pgx.Row) (Post, error) {
	var p Post
	if err := row.Scan(&p.ID, &p.AuthorID, &p.AuthorName, &p.Kind, &p.Status, &p.Title, &p.Body,
		&p.Votes, &p.Comments, &p.Voted, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Post{}, err
	}
	p.KindLabel = KindLabels[p.Kind]
	p.StatusLabel = StatusLabels[p.Status]
	return p, nil
}

// List returns the posts matching opts.
func (s *Service) List(ctx context.Context, opts ListOptions) ([]Post, error) {
	q := `SELECT ` + postColumns + ` FROM feedback_posts p WHERE TRUE`
	args := []any{opts.Viewer}
	if valid(Kinds, opts.Kind) {
		args = append(args, opts.Kind)
		q += fmt.Sprintf(" AND p.kind = $%d", len(args))
	}
	if valid(Statuses, opts.Status) {
		args = append(args, opts.Status)
		q += fmt.Sprintf(" AND p.status = $%d", len(args))
	}
	// 賛同順でも同数はいくらでも出るので、新しい順を第2キーにして並びを安定させる。
	if opts.Sort == "votes" {
		q += ` ORDER BY (SELECT COUNT(*) FROM feedback_votes v WHERE v.post_id = p.id) DESC, p.created_at DESC`
	} else {
		q += ` ORDER BY p.created_at DESC`
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	defer rows.Close()
	out := []Post{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, fmt.Errorf("scan post: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get returns one post with its comments.
func (s *Service) Get(ctx context.Context, id, viewer int64) (Detail, error) {
	p, err := scanPost(s.pool.QueryRow(ctx,
		`SELECT `+postColumns+` FROM feedback_posts p WHERE p.id = $2`, viewer, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, fmt.Errorf("get post: %w", err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, author_id, author_name, is_staff, body, created_at
		 FROM feedback_comments WHERE post_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return Detail{}, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()
	d := Detail{Post: p, Comments: []Comment{}}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.AuthorID, &c.AuthorName, &c.IsStaff, &c.Body, &c.CreatedAt); err != nil {
			return Detail{}, fmt.Errorf("scan comment: %w", err)
		}
		d.Comments = append(d.Comments, c)
	}
	return d, rows.Err()
}

// trimTo cuts a field to its limit after trimming spaces, and rejects empties.
func trimTo(v, what string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", &ErrValidation{Message: what + "を入力してください。"}
	}
	if len([]rune(v)) > max {
		return "", &ErrValidation{Message: fmt.Sprintf("%sは%d文字までです。", what, max)}
	}
	return v, nil
}

// Create posts a new entry. The caller has already checked that the player may
// post (logged in, not a guest).
func (s *Service) Create(ctx context.Context, authorID int64, kind, title, body string) (int64, error) {
	if !valid(Kinds, kind) {
		return 0, &ErrValidation{Message: "種別を選んでください。"}
	}
	title, err := trimTo(title, "タイトル", MaxTitle)
	if err != nil {
		return 0, err
	}
	body, err = trimTo(body, "内容", MaxBody)
	if err != nil {
		return 0, err
	}
	var name string
	if err := s.pool.QueryRow(ctx,
		`SELECT display_name FROM players WHERE id = $1 AND deleted_at IS NULL`, authorID).
		Scan(&name); err != nil {
		return 0, fmt.Errorf("author: %w", err)
	}
	// 連投よけ。同じ人が続けて投げると一覧が埋まる。
	var last *time.Time
	if err := s.pool.QueryRow(ctx,
		`SELECT MAX(created_at) FROM feedback_posts WHERE author_id = $1`, authorID).Scan(&last); err != nil {
		return 0, fmt.Errorf("last post: %w", err)
	}
	if last != nil && time.Since(*last) < PostInterval {
		return 0, &ErrValidation{Message: "投稿の間隔が短すぎます。少し待ってからにしてください。"}
	}
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO feedback_posts (author_id, author_name, kind, title, body)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		authorID, name, kind, title, body).Scan(&id); err != nil {
		return 0, fmt.Errorf("create post: %w", err)
	}
	return id, nil
}

// CommentResult tells the caller who to notify: the post's author, when someone
// else (typically staff) replied.
type CommentResult struct {
	PostID      int64
	PostTitle   string
	NotifyOwner *int64
}

// Comment adds a reply. isStaff is stamped onto the row so an admin's reply keeps
// reading as one later.
func (s *Service) Comment(ctx context.Context, postID, authorID int64, isStaff bool, body string) (CommentResult, error) {
	body, err := trimTo(body, "コメント", MaxComment)
	if err != nil {
		return CommentResult{}, err
	}
	var name string
	if err := s.pool.QueryRow(ctx,
		`SELECT display_name FROM players WHERE id = $1 AND deleted_at IS NULL`, authorID).
		Scan(&name); err != nil {
		return CommentResult{}, fmt.Errorf("author: %w", err)
	}
	var res CommentResult
	var ownerID *int64
	err = s.pool.QueryRow(ctx,
		`SELECT id, title, author_id FROM feedback_posts WHERE id = $1`, postID).
		Scan(&res.PostID, &res.PostTitle, &ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommentResult{}, ErrNotFound
	}
	if err != nil {
		return CommentResult{}, fmt.Errorf("post: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO feedback_comments (post_id, author_id, author_name, is_staff, body)
		 VALUES ($1, $2, $3, $4, $5)`, postID, authorID, name, isStaff, body); err != nil {
		return CommentResult{}, fmt.Errorf("create comment: %w", err)
	}
	// 自分の投稿に自分で足したときは知らせない。
	if ownerID != nil && *ownerID != authorID {
		res.NotifyOwner = ownerID
	}
	return res, nil
}

// Vote agrees with a post (1人1票)。Voting again takes the vote back.
func (s *Service) Vote(ctx context.Context, postID, playerID int64) (voted bool, err error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM feedback_votes WHERE post_id = $1 AND player_id = $2`, postID, playerID)
	if err != nil {
		return false, fmt.Errorf("unvote: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return false, nil
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO feedback_votes (post_id, player_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`, postID, playerID); err != nil {
		return false, fmt.Errorf("vote: %w", err)
	}
	return true, nil
}

// SetStatus moves a post's status. Admin only (checked by the caller).
func (s *Service) SetStatus(ctx context.Context, postID int64, status string) error {
	if !valid(Statuses, status) {
		return &ErrValidation{Message: "その状態にはできません。"}
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE feedback_posts SET status = $2, updated_at = now() WHERE id = $1`, postID, status)
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePost removes a post. The author may delete their own; an admin may
// delete any.
func (s *Service) DeletePost(ctx context.Context, postID, playerID int64, isAdmin bool) error {
	var ownerID *int64
	err := s.pool.QueryRow(ctx, `SELECT author_id FROM feedback_posts WHERE id = $1`, postID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	if !isAdmin && (ownerID == nil || *ownerID != playerID) {
		return &ErrForbidden{Message: "自分の投稿だけ消せます。"}
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM feedback_posts WHERE id = $1`, postID); err != nil {
		return fmt.Errorf("delete post: %w", err)
	}
	return nil
}

// DeleteComment removes a comment under the same rule as DeletePost.
func (s *Service) DeleteComment(ctx context.Context, commentID, playerID int64, isAdmin bool) error {
	var ownerID *int64
	err := s.pool.QueryRow(ctx, `SELECT author_id FROM feedback_comments WHERE id = $1`, commentID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("comment: %w", err)
	}
	if !isAdmin && (ownerID == nil || *ownerID != playerID) {
		return &ErrForbidden{Message: "自分のコメントだけ消せます。"}
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM feedback_comments WHERE id = $1`, commentID); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}
