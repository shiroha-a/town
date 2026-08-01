package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Hook is one destination.
type Hook struct {
	ID    int64  `json:"id"`
	URL   string `json:"url"`
	Label string `json:"label"`
	// Enabled が false の宛先には積まない(既に積んだぶんも送らない)。
	Enabled bool `json:"enabled"`
	// Events が空なら「すべて」。
	Events     []Event    `json:"events"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSentAt *time.Time `json:"last_sent_at"`
	LastStatus *int       `json:"last_status"`
	LastError  string     `json:"last_error"`
	// Pending はまだ送れていない控えの数。設定画面で詰まりに気付くため。
	Pending int `json:"pending"`
}

// ErrValidation wraps a user-facing validation failure.
type ErrValidation struct{ Message string }

func (e *ErrValidation) Error() string { return e.Message }

// ErrNotFound means no such destination.
var ErrNotFound = errors.New("webhook not found")

// List returns every destination.
func (s *Service) List(ctx context.Context) ([]Hook, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id, w.url, w.label, w.enabled, w.events, w.created_at,
		       w.last_sent_at, w.last_status, w.last_error,
		       (SELECT count(*) FROM webhook_outbox o
		         WHERE o.webhook_id = w.id AND o.delivered_at IS NULL AND o.attempts < $1)
		  FROM webhooks w ORDER BY w.id`, maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer rows.Close()
	out := []Hook{}
	for rows.Next() {
		var h Hook
		var raw []byte
		if err := rows.Scan(&h.ID, &h.URL, &h.Label, &h.Enabled, &raw, &h.CreatedAt,
			&h.LastSentAt, &h.LastStatus, &h.LastError, &h.Pending); err != nil {
			return nil, fmt.Errorf("scan webhook: %w", err)
		}
		h.Events = []Event{}
		_ = json.Unmarshal(raw, &h.Events)
		out = append(out, h)
	}
	return out, rows.Err()
}

// Create adds a destination.
func (s *Service) Create(ctx context.Context, rawURL, label string, events []Event, enabled bool) (*Hook, error) {
	clean, err := validateURL(rawURL)
	if err != nil {
		return nil, err
	}
	ev, err := normalizeEvents(events)
	if err != nil {
		return nil, err
	}
	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO webhooks (url, label, enabled, events) VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id`, clean, strings.TrimSpace(label), enabled, ev).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert webhook: %w", err)
	}
	return s.Get(ctx, id)
}

// Update replaces a destination's settings.
func (s *Service) Update(ctx context.Context, id int64, rawURL, label string, events []Event, enabled bool) (*Hook, error) {
	clean, err := validateURL(rawURL)
	if err != nil {
		return nil, err
	}
	ev, err := normalizeEvents(events)
	if err != nil {
		return nil, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE webhooks SET url = $2, label = $3, enabled = $4, events = $5::jsonb
		 WHERE id = $1`, id, clean, strings.TrimSpace(label), enabled, ev)
	if err != nil {
		return nil, fmt.Errorf("update webhook: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
}

// Get returns one destination.
func (s *Service) Get(ctx context.Context, id int64) (*Hook, error) {
	var h Hook
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT w.id, w.url, w.label, w.enabled, w.events, w.created_at,
		       w.last_sent_at, w.last_status, w.last_error,
		       (SELECT count(*) FROM webhook_outbox o
		         WHERE o.webhook_id = w.id AND o.delivered_at IS NULL AND o.attempts < $2)
		  FROM webhooks w WHERE w.id = $1`, id, maxAttempts).
		Scan(&h.ID, &h.URL, &h.Label, &h.Enabled, &raw, &h.CreatedAt,
			&h.LastSentAt, &h.LastStatus, &h.LastError, &h.Pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get webhook: %w", err)
	}
	h.Events = []Event{}
	_ = json.Unmarshal(raw, &h.Events)
	return &h, nil
}

// Delete removes a destination (and its queued notices, via ON DELETE CASCADE).
func (s *Service) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Test queues a試し送り for one destination. 宛先を登録したその場で
// 「届くか」を確かめられるようにする。
func (s *Service) Test(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO webhook_outbox (webhook_id, event, severity, title, body)
		SELECT id, 'test', 'info', '試し送り', 'この宛先に通知が届きます。'
		  FROM webhooks WHERE id = $1 AND enabled`, id)
	if err != nil {
		return fmt.Errorf("queue test: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return &ErrValidation{Message: "宛先が見つからないか、無効になっています。"}
	}
	return nil
}

// validateURL keeps the destination to a public https endpoint. スキームを
// httpsに限るのは、通知の中身(住民名・凍結の理由など)が平文で流れないように
// するため。IPやポートを弾くのはMiAuthのインスタンス指定と同じ考え方で、
// 実際の接続時にも miauth.GuardedTransport が内部アドレスを拒む。
func validateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", &ErrValidation{Message: "URLを入力してください。"}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", &ErrValidation{Message: "URLの形が正しくありません。"}
	}
	if u.Scheme != "https" {
		return "", &ErrValidation{Message: "httpsのURLだけ登録できます。"}
	}
	if u.Host == "" || u.User != nil {
		return "", &ErrValidation{Message: "URLの形が正しくありません。"}
	}
	if u.Port() != "" {
		return "", &ErrValidation{Message: "ポート付きのURLは登録できません。"}
	}
	return u.String(), nil
}

// normalizeEvents drops unknown names and returns the JSON to store.
func normalizeEvents(events []Event) ([]byte, error) {
	known := make([]Event, 0, len(events))
	for _, e := range events {
		if slices.ContainsFunc(AllEvents, func(i EventInfo) bool { return i.Event == e }) &&
			!slices.Contains(known, e) {
			known = append(known, e)
		}
	}
	raw, err := json.Marshal(known)
	if err != nil {
		return nil, fmt.Errorf("marshal events: %w", err)
	}
	return raw, nil
}
