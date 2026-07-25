// Package settings holds the runtime-tunable game settings. They are seeded from
// default.yml at first boot, persisted in the DB, and editable by admins at
// runtime. The web process updates its in-memory copy on Set; the worker process
// (separate) calls Reload each tick to pick up changes.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Game is the set of admin-editable game settings.
type Game struct {
	InitialMoney             int64        `json:"initial_money"`
	DailyInterestPermille    int          `json:"daily_interest_permille"`
	EnergyRecoverySec        int          `json:"energy_recovery_sec"`
	NouRecoverySec           int          `json:"nou_recovery_sec"`
	SatietyDecaySec          int          `json:"satiety_decay_sec"`
	ConditionEvalIntervalMin int          `json:"condition_eval_interval_min"`
	WorkIntervalMin          int          `json:"work_interval_min"`
	DebugNoCooldown          bool         `json:"debug_no_cooldown"`
	DepartDailyCount         int          `json:"depart_daily_count"`
	SyokudouDailyCount       int          `json:"syokudou_daily_count"`
	HanbaiDailyCount         int          `json:"hanbai_daily_count"` // 自販機で毎日陳列する品数
	ItemKindLimit            int          `json:"item_kind_limit"`    // 所持できるアイテムの種類上限
	StockAdjust              int          `json:"stock_adjust"`       // 店頭在庫の割り算倍率(実在庫=ceil(標準在庫/倍率))
	MoveMaigoEnabled         bool         `json:"move_maigo_enabled"` // 街移動の迷子(徒歩)を有効化(レガシー既定OFF)
	MoveWalkSecs             int          `json:"move_walk_secs"`     // 徒歩の街移動にかかる秒数(0以下で既定10)
	MoveBusSecs              int          `json:"move_bus_secs"`      // バスの街移動にかかる秒数(0以下で既定5)
	Towns                    []TownConfig `json:"towns"`              // 街の一覧(名前・地価)。数は要素数
	// InstancePolicy: 参加できるMisskeyインスタンスの方針。
	// "blacklist"(既定)=blockリストに無ければ通す / "whitelist"=allowリストに有るものだけ通す。
	InstancePolicy string `json:"instance_policy"`
}

// TownConfig is one configurable town: its name and land price (万円)。街番号は
// 並び順(0始まり)で決まる。Hidden はワープ不可の隠し町。
type TownConfig struct {
	Name      string `json:"name"`
	LandPrice int    `json:"land_price"`
	Hidden    bool   `json:"hidden"`
}

// Store is a thread-safe, DB-backed holder of the current game settings.
type Store struct {
	pool *pgxpool.Pool
	mu   sync.RWMutex
	g    Game
}

// NewStore loads settings from the DB, seeding them from defaults if absent.
func NewStore(ctx context.Context, pool *pgxpool.Pool, defaults Game) (*Store, error) {
	s := &Store{pool: pool, g: defaults}
	var data []byte
	err := pool.QueryRow(ctx, `SELECT game FROM app_settings WHERE id = 1`).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		b, _ := json.Marshal(defaults)
		if _, e := pool.Exec(ctx,
			`INSERT INTO app_settings (id, game) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`, b); e != nil {
			return nil, fmt.Errorf("seed settings: %w", e)
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	var g Game
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}
	s.g = g
	return s, nil
}

// Get returns a snapshot of the current settings.
func (s *Store) Get() Game {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.g
}

// Set persists and applies new settings (used by the admin API).
func (s *Store) Set(ctx context.Context, g Game) error {
	b, err := json.Marshal(g)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE app_settings SET game = $1, updated_at = now() WHERE id = 1`, b); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	s.mu.Lock()
	s.g = g
	s.mu.Unlock()
	return nil
}

// Reload re-reads settings from the DB (used by the worker each tick so runtime
// changes made via the web process take effect).
func (s *Store) Reload(ctx context.Context) error {
	var data []byte
	if err := s.pool.QueryRow(ctx, `SELECT game FROM app_settings WHERE id = 1`).Scan(&data); err != nil {
		return fmt.Errorf("reload settings: %w", err)
	}
	var g Game
	if err := json.Unmarshal(data, &g); err != nil {
		return fmt.Errorf("parse settings: %w", err)
	}
	s.mu.Lock()
	s.g = g
	s.mu.Unlock()
	return nil
}
