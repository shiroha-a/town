// Package settings holds the game settings. They are seeded from the defaults
// below at first boot, persisted in the DB, and edited by admins from the
// in-game admin screen — the config file holds only infrastructure (DB/Redis/
// port), never gameplay values. The web process updates its in-memory copy on
// Set; the worker process (separate) calls Reload each tick to pick up changes.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Game is the set of admin-editable game settings.
type Game struct {
	// Timezone / DayBoundaryHour decide when a "game day" rolls over (利息・日次処理)。
	// 起動時に読むため、変更の反映には再起動が要る。
	Timezone                 string       `json:"timezone"`
	DayBoundaryHour          int          `json:"day_boundary_hour"`
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
	// GuestEnabled: お試しプレイ(ゲストログイン)を受け付けるか。
	GuestEnabled bool `json:"guest_enabled"`
	// GuestLifetimeMin: ゲストのデータを消すまでの分数。作成からの経過で数える。
	GuestLifetimeMin int `json:"guest_lifetime_min"`
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

// Defaults are the values a fresh install starts from. レガシー準拠の初期値で、
// 以後の変更は管理画面から行う(設定ファイルには持たない)。
func Defaults() Game {
	return Game{
		Timezone:                 "Asia/Tokyo",
		DayBoundaryHour:          5,      // 日付の切り替わり(利息・日次処理)はAM5:00
		InitialMoney:             500000, // 新規登録時の初期所持金(円)
		DailyInterestPermille:    5,      // 貯金の日次利息(パーミル。5=0.5%、切り捨て)
		EnergyRecoverySec:        60,     // 身体パワー1回復に必要な秒数
		NouRecoverySec:           60,     // 頭脳パワー1回復に必要な秒数
		SatietyDecaySec:          300,    // 空腹値が1減るのに必要な秒数
		ConditionEvalIntervalMin: 10,     // 病気指数のコンディション評価間隔(分)
		WorkIntervalMin:          3,      // 就労のクールタイム(分)
		DebugNoCooldown:          false,  // 各種クールタイムを無視する(開発用)
		DepartDailyCount:         100,    // デパートで毎日陳列する品数(0以下=全件)
		SyokudouDailyCount:       9,      // 食堂で毎日陳列する品数(0以下=全件)
		HanbaiDailyCount:         3,      // 自販機で毎日陳列する品数(0以下=全件)
		ItemKindLimit:            25,     // 所持できるアイテムの種類上限(0以下=無制限)
		StockAdjust:              2,      // 店頭在庫の割り算倍率
		MoveMaigoEnabled:         false,  // 徒歩移動の迷子(レガシー既定OFF)
		MoveWalkSecs:             10,     // 徒歩の街移動にかかる秒数
		MoveBusSecs:              5,      // バスの街移動にかかる秒数
	}
}

// Location resolves Timezone, falling back to UTC when it is unusable.
func (g Game) Location() (*time.Location, error) {
	loc, err := time.LoadLocation(g.Timezone)
	if err != nil {
		return time.UTC, err
	}
	return loc, nil
}

// ErrInvalid marks a rejected settings update (管理画面からの入力エラー)。
var ErrInvalid = errors.New("invalid settings")

// Validate rejects values that would break time handling. 管理画面から編集できる
// ようになった以上、壊れた値がそのまま保存されないようにする。
func (g Game) Validate() error {
	if _, err := time.LoadLocation(g.Timezone); err != nil {
		return fmt.Errorf("%w: タイムゾーン %q は解釈できません(例: Asia/Tokyo)", ErrInvalid, g.Timezone)
	}
	if g.DayBoundaryHour < 0 || g.DayBoundaryHour > 23 {
		return fmt.Errorf("%w: 日付の切り替わりは0〜23時で指定してください", ErrInvalid)
	}
	return nil
}

// NewStatic returns a Store that holds the given settings in memory only. DBを
// 必要としないので、設定値だけが要るテストや、単発の計算に使う。
func NewStatic(g Game) *Store {
	return &Store{g: g}
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
	if err := g.Validate(); err != nil {
		return err
	}
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
