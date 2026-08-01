package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/shiroha-a/town/internal/miauth"
)

// 送信まわりの上限。
const (
	// maxAttempts は諦めるまでの回数。1,2,4,8,16分の間隔で試す。
	maxAttempts = 5
	// batchSize は1tickで送る数。詰まっても他の処理を止めないよう小さく。
	batchSize      = 20
	requestTimeout = 10 * time.Second
	// keepDelivered は配送済みの控えを残す日数。
	keepDelivered = 7
)

// httpClient は宛先が持ち込みのURLなので、MiAuthと同じSSRFガードを通す。
// 内部アドレスへ向けられると、この街を踏み台に社内へ叩きに行けてしまう。
var httpClient = &http.Client{
	Transport:     miauth.GuardedTransport(),
	Timeout:       requestTimeout,
	CheckRedirect: miauth.GuardedRedirect,
}

// pending is one queued delivery.
type pending struct {
	id       int64
	hookID   int64
	url      string
	event    Event
	severity Severity
	title    string
	body     string
	fields   []Field
	attempts int
}

// Deliver sends the queued notices that are due. worker から毎tick呼ぶ。
// 戻り値は送れた数。
func (s *Service) Deliver(ctx context.Context) (int, error) {
	if s == nil || s.pool == nil {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT o.id, o.webhook_id, w.url, o.event, o.severity, o.title, o.body, o.fields, o.attempts
		  FROM webhook_outbox o JOIN webhooks w ON w.id = o.webhook_id
		 WHERE o.delivered_at IS NULL AND o.attempts < $1 AND o.next_try_at <= now()
		   AND w.enabled
		 ORDER BY o.id
		 LIMIT $2`, maxAttempts, batchSize)
	if err != nil {
		return 0, fmt.Errorf("load outbox: %w", err)
	}
	var list []pending
	for rows.Next() {
		var p pending
		var raw []byte
		if err := rows.Scan(&p.id, &p.hookID, &p.url, &p.event, &p.severity,
			&p.title, &p.body, &raw, &p.attempts); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan outbox: %w", err)
		}
		_ = json.Unmarshal(raw, &p.fields)
		list = append(list, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read outbox: %w", err)
	}

	sent := 0
	for _, p := range list {
		status, retryAfter, err := s.send(ctx, p)
		if err == nil {
			if e := s.markDone(ctx, p, status); e != nil {
				return sent, e
			}
			sent++
			continue
		}
		if e := s.markFailed(ctx, p, status, err, retryAfter); e != nil {
			return sent, e
		}
	}
	return sent, nil
}

// send posts one notice. status は応答が返ったときだけ非0。
func (s *Service) send(ctx context.Context, p pending) (status int, retryAfter time.Duration, err error) {
	payload, err := json.Marshal(discordPayload(p))
	if err != nil {
		return 0, 0, fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(payload))
	if err != nil {
		return 0, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", miauth.UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	// 応答は捨てるが、読み切らないと接続を使い回せない。
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, 0, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		// Discord は Retry-After(秒)を返す。言われたとおりに待つ。
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, e := strconv.ParseFloat(v, 64); e == nil && secs > 0 {
				retryAfter = time.Duration(secs * float64(time.Second))
			}
		}
	}
	return resp.StatusCode, retryAfter, fmt.Errorf("HTTP %d %s", resp.StatusCode, truncate(string(snippet), 200))
}

func (s *Service) markDone(ctx context.Context, p pending, status int) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE webhook_outbox SET delivered_at = now(), attempts = attempts + 1, last_error = ''
		 WHERE id = $1`, p.id); err != nil {
		return fmt.Errorf("mark delivered: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE webhooks SET last_sent_at = now(), last_status = $2, last_error = ''
		 WHERE id = $1`, p.hookID, status); err != nil {
		return fmt.Errorf("record success: %w", err)
	}
	return nil
}

// markFailed schedules the next try. 待ち時間は 1,2,4,8,16分(429で指定が
// あればそちらを優先)。回数を使い切った控えは残したまま、送信対象から外れる。
func (s *Service) markFailed(ctx context.Context, p pending, status int, cause error, retryAfter time.Duration) error {
	wait := time.Duration(1<<p.attempts) * time.Minute
	if retryAfter > 0 {
		wait = retryAfter
	}
	msg := truncate(cause.Error(), 500)
	if _, err := s.pool.Exec(ctx, `
		UPDATE webhook_outbox
		   SET attempts = attempts + 1, next_try_at = now() + $2::interval, last_error = $3
		 WHERE id = $1`, p.id, wait.String(), msg); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	var st *int
	if status != 0 {
		st = &status
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE webhooks SET last_sent_at = now(), last_status = $2, last_error = $3
		 WHERE id = $1`, p.hookID, st, msg); err != nil {
		return fmt.Errorf("record failure: %w", err)
	}
	return nil
}

// Purge drops delivered notices older than keepDelivered days, and the ones
// that used up their attempts. 日次から呼ぶ。
func (s *Service) Purge(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM webhook_outbox
		 WHERE (delivered_at IS NOT NULL AND delivered_at < now() - make_interval(days => $1))
		    OR (delivered_at IS NULL AND attempts >= $2 AND created_at < now() - make_interval(days => $1))`,
		keepDelivered, maxAttempts); err != nil {
		return fmt.Errorf("purge outbox: %w", err)
	}
	return nil
}

// ── Discord の形 ─────────────────────────────────────────

// 重みごとの色。Discord は10進のRGB。
const (
	colorInfo  = 0x2ECC71
	colorWarn  = 0xE67E22
	colorError = 0xE74C3C
)

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      struct {
		Text string `json:"text"`
	} `json:"footer"`
	Timestamp string `json:"timestamp"`
}

type discordBody struct {
	Embeds []discordEmbed `json:"embeds"`
}

// discordPayload builds the Discord-compatible body. Slack など他所でも
// 「JSONを受ける」だけなら形は変えずに済む。
func discordPayload(p pending) discordBody {
	e := discordEmbed{
		Title: p.title,
		// Discord の説明は4096字まで。長い本文は切る。
		Description: truncate(p.body, 3800),
		Color:       colorFor(p.severity),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	e.Footer.Text = "TOWN / " + string(p.event)
	for _, f := range p.fields {
		e.Fields = append(e.Fields, discordField{
			Name: f.Name, Value: truncate(f.Value, 1000), Inline: true,
		})
	}
	return discordBody{Embeds: []discordEmbed{e}}
}

func colorFor(s Severity) int {
	switch s {
	case SeverityError:
		return colorError
	case SeverityWarn:
		return colorWarn
	default:
		return colorInfo
	}
}

// truncate cuts a string to n runes. 文字の途中で切って壊さないようruneで数える。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
