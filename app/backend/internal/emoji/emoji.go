// Package emoji lets residents use their instance's custom emoji in the town's
// text (家の掲示板・会社BBS・あいさつ).
//
// Design: .tmp/design_emoji.md
//
// 二つの原則で組んでいる:
//   - 本文には `:name@host:` だけを保存する。URLも生HTMLも入れない。後からURLが
//     変わっても追随でき、キャッシュを消せば全投稿から一斉に消える。
//   - 外部への問い合わせは「これまでに一度でも使われた絵文字の種類数」にしか
//     比例させない。表示のたびに相手インスタンスを叩かない。
package emoji

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/miauth"
)

const (
	// listTTL mirrors the upstream cacheSec of /api/emojis.
	listTTL = time.Hour
	// rejectTTL keeps a refusal around long enough to stop a retry loop, but
	// short enough that fixing the license upstream takes effect the same day.
	rejectTTL = time.Hour
	// MaxPerPost caps how many custom emoji one post may carry.
	MaxPerPost = 20
)

// Rejection reasons, also sent to the UI so it can explain the refusal.
const (
	ReasonNoLicense = "no_license"
	ReasonSensitive = "sensitive"
	ReasonLocalOnly = "local_only"
	ReasonNotFound  = "not_found"
)

// ErrRejected means the emoji exists but may not be used here.
var ErrRejected = errors.New("emoji rejected")

// Service resolves and caches custom emoji.
type Service struct {
	pool *pgxpool.Pool
	mi   *miauth.Client

	mu    sync.Mutex
	lists map[string]cachedList // host -> ピッカー用の一覧
}

type cachedList struct {
	at    time.Time
	items []miauth.EmojiSimple
}

// New builds the service.
func New(pool *pgxpool.Pool, mi *miauth.Client) *Service {
	return &Service{pool: pool, mi: mi, lists: map[string]cachedList{}}
}

// Emoji is a usable custom emoji.
type Emoji struct {
	Host     string `json:"host"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	License  string `json:"license"`
	Category string `json:"category"`
}

// Shortcode is how the emoji appears in text: :name@host:.
func (e Emoji) Shortcode() string { return ":" + e.Name + "@" + e.Host + ":" }

// refPattern matches :name@host: . Misskey shortcodes are [a-z0-9_]+, and the
// host is a normal domain.
var refPattern = regexp.MustCompile(`:([a-zA-Z0-9_+-]+)@([a-zA-Z0-9.-]+):`)

// Ref is one :name@host: occurrence.
type Ref struct {
	Name string
	Host string
}

// Refs returns the distinct emoji referenced by a text, in order.
func Refs(text string) []Ref {
	var out []Ref
	seen := map[string]bool{}
	for _, m := range refPattern.FindAllStringSubmatch(text, -1) {
		// ホストの大文字小文字は同一視する(重複判定もキャッシュキーも小文字基準)。
		host := strings.ToLower(m[2])
		key := m[1] + "@" + host
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Ref{Name: m[1], Host: host})
	}
	return out
}

// CountRefs counts emoji occurrences (not distinct) for the per-post cap.
func CountRefs(text string) int {
	return len(refPattern.FindAllString(text, -1))
}

// List returns an instance's emoji for the picker. The entries whose flags are
// present and disqualifying are dropped here; the rest are only provisional —
// Resolve makes the real decision, because the license we require is not part
// of this response.
func (s *Service) List(ctx context.Context, host string) ([]miauth.EmojiSimple, error) {
	s.mu.Lock()
	if c, ok := s.lists[host]; ok && time.Since(c.at) < listTTL {
		s.mu.Unlock()
		return c.items, nil
	}
	s.mu.Unlock()

	items, err := s.mi.Emojis(ctx, host)
	if err != nil {
		return nil, err
	}
	kept := make([]miauth.EmojiSimple, 0, len(items))
	for _, e := range items {
		if e.URL == "" || e.Name == "" {
			continue
		}
		// 欠けている場合は判断を保留する(欠落をfalse扱いにしない)。
		if e.LocalOnly != nil && *e.LocalOnly {
			continue
		}
		if e.IsSensitive != nil && *e.IsSensitive {
			continue
		}
		kept = append(kept, e)
	}
	s.mu.Lock()
	s.lists[host] = cachedList{at: time.Now(), items: kept}
	s.mu.Unlock()
	return kept, nil
}

// Resolve decides whether one emoji may be used, fetching it in full the first
// time. An approved emoji is cached forever (until Forget), a refused one only
// briefly.
func (s *Service) Resolve(ctx context.Context, host, name string) (*Emoji, error) {
	host = strings.ToLower(host)
	if e, ok, err := s.cached(ctx, host, name); err != nil {
		return nil, err
	} else if ok {
		return e, nil
	}
	if reason, ok, err := s.rejected(ctx, host, name); err != nil {
		return nil, err
	} else if ok {
		return nil, fmt.Errorf("%w: %s", ErrRejected, reason)
	}

	d, err := s.mi.Emoji(ctx, host, name)
	if err != nil {
		// 見つからない/応答しないは短期の否定キャッシュに入れる。
		_ = s.reject(ctx, host, name, ReasonNotFound)
		return nil, fmt.Errorf("%w: %s", ErrRejected, ReasonNotFound)
	}
	if reason := verdict(d); reason != "" {
		_ = s.reject(ctx, host, name, reason)
		return nil, fmt.Errorf("%w: %s", ErrRejected, reason)
	}

	e := &Emoji{Host: host, Name: d.Name, URL: d.URL, License: d.License, Category: d.Category}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO misskey_emojis (host, name, url, license, category, fetched_at)
		 VALUES ($1,$2,$3,$4,$5, now())
		 ON CONFLICT (host, name) DO UPDATE SET
		   url = EXCLUDED.url, license = EXCLUDED.license,
		   category = EXCLUDED.category, fetched_at = now()`,
		e.Host, e.Name, e.URL, e.License, e.Category); err != nil {
		return nil, fmt.Errorf("save emoji: %w", err)
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM misskey_emoji_rejects WHERE host = $1 AND name = $2`, host, name)
	return e, nil
}

// verdict returns the reason an emoji may not be used, or "" when it may.
// 作者のインスタンス外へ持ち出して表示する以上、ライセンス表明のあるものに限る。
func verdict(d *miauth.EmojiDetailed) string {
	switch {
	case d.LocalOnly:
		return ReasonLocalOnly
	case d.IsSensitive:
		return ReasonSensitive
	case strings.TrimSpace(d.License) == "":
		return ReasonNoLicense
	}
	return ""
}

// ReasonMessage explains a refusal to the player.
func ReasonMessage(reason string) string {
	switch reason {
	case ReasonNoLicense:
		return "この絵文字はライセンスが設定されていないため使えません。"
	case ReasonSensitive:
		return "この絵文字はセンシティブ指定のため使えません。"
	case ReasonLocalOnly:
		return "この絵文字は連合しない設定のため使えません。"
	case ReasonNotFound:
		return "その絵文字は見つかりませんでした。"
	}
	return "この絵文字は使えません。"
}

// ReasonOf pulls the reason out of an ErrRejected error.
func ReasonOf(err error) string {
	if !errors.Is(err, ErrRejected) {
		return ""
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return ""
}

func (s *Service) cached(ctx context.Context, host, name string) (*Emoji, bool, error) {
	var e Emoji
	err := s.pool.QueryRow(ctx,
		`SELECT host, name, url, license, category FROM misskey_emojis
		  WHERE host = $1 AND name = $2`, host, name).
		Scan(&e.Host, &e.Name, &e.URL, &e.License, &e.Category)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read emoji: %w", err)
	}
	return &e, true, nil
}

func (s *Service) rejected(ctx context.Context, host, name string) (string, bool, error) {
	var reason string
	err := s.pool.QueryRow(ctx,
		`SELECT reason FROM misskey_emoji_rejects
		  WHERE host = $1 AND name = $2 AND checked_at > now() - $3::interval`,
		host, name, rejectTTL.String()).Scan(&reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read emoji reject: %w", err)
	}
	return reason, true, nil
}

func (s *Service) reject(ctx context.Context, host, name, reason string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO misskey_emoji_rejects (host, name, reason, checked_at)
		 VALUES ($1,$2,$3, now())
		 ON CONFLICT (host, name) DO UPDATE SET reason = EXCLUDED.reason, checked_at = now()`,
		host, name, reason)
	return err
}

// Used returns every emoji approved so far, for the client to render posts with.
// Only ever-used emoji land in this table, so it stays small; rendering never
// triggers an external fetch — an unknown shortcode stays as text.
func (s *Service) Used(ctx context.Context) ([]Emoji, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT host, name, url, license, category FROM misskey_emojis ORDER BY host, name`)
	if err != nil {
		return nil, fmt.Errorf("list emojis: %w", err)
	}
	defer rows.Close()
	out := []Emoji{}
	for rows.Next() {
		var e Emoji
		if err := rows.Scan(&e.Host, &e.Name, &e.URL, &e.License, &e.Category); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
