// Package serial implements シリアルコード — admin-issued redemption codes that
// hand out items and/or stat effects, once per player. It replaces the legacy
// 特典 (tokuten.cgi), which could only hold two hard-coded 合言葉; here any
// number of codes can be issued, each recording who redeemed it.
package serial

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/rng"
)

// codeAlphabet omits characters that are easy to misread when a code is copied
// by hand (0/O, 1/I/L).
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// CodeGroups/CodeGroupLen shape generated codes as XXXX-XXXX-XXXX.
const (
	CodeGroups   = 3
	CodeGroupLen = 4
)

// Code is one redemption code as the admin sees it.
type Code struct {
	ID             int64      `json:"id"`
	Code           string     `json:"code"`
	Label          string     `json:"label"`
	Message        string     `json:"message"`
	Effect         []byte     `json:"-"`
	EffectRaw      string     `json:"effect"` // JSON文字列としてそのまま往復させる
	RewardItemID   *int64     `json:"reward_item_id"`
	RewardItemName string     `json:"reward_item_name"`
	RewardItemUses int        `json:"reward_item_uses"`
	MaxUses        int        `json:"max_uses"`
	UsedCount      int        `json:"used_count"`
	StartsAt       *time.Time `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at"`
	Enabled        bool       `json:"enabled"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Use is one redemption record (who used a code, and when).
type Use struct {
	PlayerID   int64     `json:"player_id"`
	PlayerName string    `json:"player_name"`
	UsedAt     time.Time `json:"used_at"`
}

// Service reads and writes serial codes.
type Service struct {
	pool *pgxpool.Pool
	rng  *rng.Rand
}

// New returns a serial-code service backed by pool.
func New(pool *pgxpool.Pool, r *rng.Rand) *Service {
	return &Service{pool: pool, rng: r}
}

// Normalize canonicalises user input: upper-case, hyphens and spaces stripped.
// Codes are compared in this form so "town-1234" matches "TOWN1234".
func Normalize(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		if r == '-' || r == ' ' || r == '　' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Generate builds a fresh random code in XXXX-XXXX-XXXX form (stored normalized).
func (s *Service) Generate() string {
	out := make([]byte, 0, CodeGroups*CodeGroupLen)
	for range CodeGroups * CodeGroupLen {
		out = append(out, codeAlphabet[s.rng.IntN(len(codeAlphabet))])
	}
	return string(out)
}

// Display formats a stored code back into the hyphenated form for the UI.
func Display(code string) string {
	var parts []string
	for i := 0; i < len(code); i += CodeGroupLen {
		end := min(i+CodeGroupLen, len(code))
		parts = append(parts, code[i:end])
	}
	return strings.Join(parts, "-")
}

const selectCodes = `
SELECT sc.id, sc.code, sc.label, sc.message, sc.effect,
       sc.reward_item_id, COALESCE(ci.name, ''), sc.reward_item_uses,
       sc.max_uses, sc.used_count, sc.starts_at, sc.ends_at, sc.enabled, sc.created_at
FROM serial_codes sc LEFT JOIN content_items ci ON ci.id = sc.reward_item_id`

// List returns every code, newest first.
func (s *Service) List(ctx context.Context) ([]Code, error) {
	rows, err := s.pool.Query(ctx, selectCodes+` ORDER BY sc.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list codes: %w", err)
	}
	defer rows.Close()
	out := []Code{}
	for rows.Next() {
		c, err := scanCode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCode(rows pgx.Rows) (Code, error) {
	var c Code
	if err := rows.Scan(&c.ID, &c.Code, &c.Label, &c.Message, &c.Effect,
		&c.RewardItemID, &c.RewardItemName, &c.RewardItemUses,
		&c.MaxUses, &c.UsedCount, &c.StartsAt, &c.EndsAt, &c.Enabled, &c.CreatedAt); err != nil {
		return c, fmt.Errorf("scan code: %w", err)
	}
	c.Code = Display(c.Code)
	c.EffectRaw = string(c.Effect)
	return c, nil
}

// Uses lists who redeemed one code, newest first.
func (s *Service) Uses(ctx context.Context, codeID int64) ([]Use, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT player_id, player_name, used_at FROM serial_code_uses
		 WHERE code_id = $1 ORDER BY used_at DESC`, codeID)
	if err != nil {
		return nil, fmt.Errorf("list uses: %w", err)
	}
	defer rows.Close()
	out := []Use{}
	for rows.Next() {
		var u Use
		if err := rows.Scan(&u.PlayerID, &u.PlayerName, &u.UsedAt); err != nil {
			return nil, fmt.Errorf("scan use: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// fillRewardUses defaults the reward durability to one unit of the item.
// 景品アイテムを選んでも耐久が0のままだと、引き換えは成功するのに何も配られない
// (付与は remaining_uses を足す形なので0では持ち物が増えない)。管理画面の入力を
// 0のまま保存できてしまうため、ここで1個ぶんに補う。
func (s *Service) fillRewardUses(ctx context.Context, c *Code) error {
	if c.RewardItemID == nil || c.RewardItemUses > 0 {
		return nil
	}
	var durability int
	if err := s.pool.QueryRow(ctx,
		`SELECT GREATEST(durability, 1) FROM content_items WHERE id = $1`, *c.RewardItemID).
		Scan(&durability); err != nil {
		return fmt.Errorf("load reward item durability: %w", err)
	}
	c.RewardItemUses = durability
	return nil
}

// Create issues a code. An empty Code field generates a random one.
func (s *Service) Create(ctx context.Context, c Code) (*Code, error) {
	code := Normalize(c.Code)
	if code == "" {
		code = s.Generate()
	}
	effect := c.EffectRaw
	if strings.TrimSpace(effect) == "" {
		effect = "[]"
	}
	if err := s.fillRewardUses(ctx, &c); err != nil {
		return nil, err
	}
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO serial_codes (code, label, message, effect, reward_item_id, reward_item_uses,
		                           max_uses, starts_at, ends_at, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		code, c.Label, c.Message, effect, c.RewardItemID, c.RewardItemUses,
		c.MaxUses, c.StartsAt, c.EndsAt, c.Enabled).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create code: %w", err)
	}
	return s.Get(ctx, id)
}

// Update replaces an existing code's settings (the code string itself included).
func (s *Service) Update(ctx context.Context, c Code) (*Code, error) {
	code := Normalize(c.Code)
	if code == "" {
		return nil, fmt.Errorf("code is required")
	}
	effect := c.EffectRaw
	if strings.TrimSpace(effect) == "" {
		effect = "[]"
	}
	if err := s.fillRewardUses(ctx, &c); err != nil {
		return nil, err
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE serial_codes SET code = $2, label = $3, message = $4, effect = $5,
		        reward_item_id = $6, reward_item_uses = $7, max_uses = $8,
		        starts_at = $9, ends_at = $10, enabled = $11
		 WHERE id = $1`,
		c.ID, code, c.Label, c.Message, effect, c.RewardItemID, c.RewardItemUses,
		c.MaxUses, c.StartsAt, c.EndsAt, c.Enabled)
	if err != nil {
		return nil, fmt.Errorf("update code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, c.ID)
}

// Delete removes a code and its redemption records.
func (s *Service) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM serial_codes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns one code by id.
func (s *Service) Get(ctx context.Context, id int64) (*Code, error) {
	rows, err := s.pool.Query(ctx, selectCodes+` WHERE sc.id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("get code: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	c, err := scanCode(rows)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ErrNotFound is returned when a code id does not exist.
var ErrNotFound = errors.New("serial code not found")
