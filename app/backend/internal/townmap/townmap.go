// Package townmap holds the runtime-editable town map: the placement of
// facilities on the main-screen grid. It is seeded with a legacy-faithful
// default at first boot, persisted as a single JSONB row, and editable by
// admins. Every player fetches it to render the map; only admins may change it.
package townmap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Grid dimensions of the town map (columns 1..Cols, rows 0..Rows-1 = A..L).
// MaxTowns is the hard upper bound for a facility/asset's town field. The actual
// number of towns is admin-configurable (building.TownCount); this is only a
// validation ceiling so the field cannot be absurd.
const (
	Cols     = 16
	Rows     = 12
	MaxTowns = 12
)

// Facility is a single placed facility on the town map (functional layer).
// key="walk"(徒歩)/"bus"(バス) は街移動施設で、Dest が行き先の街になる。
type Facility struct {
	Key   string `json:"key"`   // ビュー遷移先 or 移動施設(walk/bus)。空不可
	Img   string `json:"img"`   // gif名(拡張子なし)。空不可
	Alt   string `json:"alt"`   // 表示名(ツールチップ)
	Town  int    `json:"town"`  // 街番号(0..Towns-1)。既定0=メイン街
	Col   int    `json:"col"`   // 1..Cols
	Row   int    `json:"row"`   // 0..Rows-1
	Dest  int    `json:"dest"`  // 移動施設の行き先の街(0..Towns-1)。非移動施設は無視
	Ready bool   `json:"ready"` // 有効なら遷移/移動可能
}

// Asset is a single placed background asset (decorative layer). It has no
// function; it just renders beneath the facility layer, one tile per cell.
type Asset struct {
	Img  string `json:"img"`  // gif名(拡張子なし)。空不可
	Town int    `json:"town"` // 街番号(0..Towns-1)。既定0=メイン街
	Col  int    `json:"col"`  // 1..Cols
	Row  int    `json:"row"`  // 0..Rows-1
}

// Default is the legacy-faithful initial layout, mirroring the placement that
// used to be hardcoded in TownView.vue.
func Default() []Facility {
	return []Facility{
		{Key: "kabu", Img: "kabu", Alt: "株取引場", Col: 2, Row: 3, Ready: true},
		{Key: "depart", Img: "depart", Alt: "中央デパート", Col: 8, Row: 3, Ready: true},
		{Key: "bank", Img: "bank", Alt: "銀行", Col: 6, Row: 4, Ready: true},
		{Key: "syokudou", Img: "syokudou", Alt: "セントラル食堂", Col: 9, Row: 5, Ready: true},
		{Key: "gym", Img: "gym", Alt: "ジム", Col: 11, Row: 9, Ready: true},
		{Key: "keiba", Img: "keiba", Alt: "競馬場", Col: 13, Row: 9, Ready: true},
		{Key: "jobchange", Img: "work", Alt: "職業安定所", Col: 2, Row: 6, Ready: true},
		{Key: "onsen", Img: "onsen", Alt: "温泉", Col: 4, Row: 7, Ready: true},
		{Key: "hospital", Img: "hospital", Alt: "中央病院", Col: 12, Row: 6, Ready: true},
		{Key: "school", Img: "school", Alt: "学校", Col: 10, Row: 7, Ready: true},
		{Key: "kyushitu", Img: "school", Alt: "教室", Col: 8, Row: 9, Ready: true},
		{Key: "kentiku", Img: "kentiku", Alt: "建設会社", Col: 13, Row: 4, Ready: true},
		{Key: "hanbai", Img: "hanbai", Alt: "自動販売機", Col: 4, Row: 4, Ready: true},
		{Key: "yakuba", Img: "yakuba", Alt: "役場（住民名鑑）", Col: 6, Row: 7, Ready: true},
		{Key: "prof", Img: "prof", Alt: "プロフィール", Col: 14, Row: 11, Ready: false},
	}
}

// Store is a thread-safe, DB-backed holder of the current town map.
type Store struct {
	pool       *pgxpool.Pool
	mu         sync.RWMutex
	facilities []Facility
	assets     []Asset
	presets    []FacilityPreset
}

// FacilityPreset is a reusable facility template (画像・表示名・遷移先) the
// admin saves once and then places onto the map by drag & drop.
type FacilityPreset struct {
	Key  string `json:"key"`  // ビュー遷移先 or 移動施設(walk/bus)。空不可
	Img  string `json:"img"`  // gif名(拡張子なし)。空不可
	Alt  string `json:"alt"`  // 表示名
	Dest int    `json:"dest"` // 移動施設の行き先の街(非移動施設は無視)
}

// NewStore loads the map from the DB, seeding it from defaults if absent.
func NewStore(ctx context.Context, pool *pgxpool.Pool, defaults []Facility) (*Store, error) {
	s := &Store{pool: pool, facilities: defaults, assets: []Asset{}, presets: []FacilityPreset{}}
	var facData, assetData, presetData []byte
	err := pool.QueryRow(ctx, `SELECT facilities, assets, facility_presets FROM town_map WHERE id = 1`).
		Scan(&facData, &assetData, &presetData)
	if errors.Is(err, pgx.ErrNoRows) {
		b, _ := json.Marshal(defaults)
		if _, e := pool.Exec(ctx,
			`INSERT INTO town_map (id, facilities) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`, b); e != nil {
			return nil, fmt.Errorf("seed town map: %w", e)
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load town map: %w", err)
	}
	var fs []Facility
	if err := json.Unmarshal(facData, &fs); err != nil {
		return nil, fmt.Errorf("parse town map: %w", err)
	}
	s.facilities = fs
	// assets列はマイグレーション後の既存行では '[]' が入る。
	if len(assetData) > 0 {
		var as []Asset
		if err := json.Unmarshal(assetData, &as); err != nil {
			return nil, fmt.Errorf("parse town assets: %w", err)
		}
		s.assets = as
	}
	if len(presetData) > 0 {
		var ps []FacilityPreset
		if err := json.Unmarshal(presetData, &ps); err != nil {
			return nil, fmt.Errorf("parse facility presets: %w", err)
		}
		s.presets = ps
	}
	return s, nil
}

// GetPresets returns a copy of the saved facility presets.
func (s *Store) GetPresets() []FacilityPreset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]FacilityPreset, len(s.presets))
	copy(out, s.presets)
	return out
}

// SetPresets validates and persists the facility preset list.
func (s *Store) SetPresets(ctx context.Context, ps []FacilityPreset) error {
	for i, p := range ps {
		if p.Key == "" {
			return fmt.Errorf("preset %d: key is required", i)
		}
		if p.Img == "" {
			return fmt.Errorf("preset %d: img is required", i)
		}
		if len([]rune(p.Alt)) > 40 {
			return fmt.Errorf("preset %d: alt too long (max 40)", i)
		}
		if p.Dest < 0 || p.Dest >= MaxTowns {
			return fmt.Errorf("preset %d: dest %d out of range 0..%d", i, p.Dest, MaxTowns-1)
		}
	}
	b, err := json.Marshal(ps)
	if err != nil {
		return fmt.Errorf("encode facility presets: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE town_map SET facility_presets = $1, updated_at = now() WHERE id = 1`, b); err != nil {
		return fmt.Errorf("save facility presets: %w", err)
	}
	s.mu.Lock()
	s.presets = ps
	s.mu.Unlock()
	return nil
}

// Get returns a copy of the current facilities.
func (s *Store) Get() []Facility {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Facility, len(s.facilities))
	copy(out, s.facilities)
	return out
}

// GetAssets returns a copy of the current background assets.
func (s *Store) GetAssets() []Asset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Asset, len(s.assets))
	copy(out, s.assets)
	return out
}

// Validate checks grid bounds, required fields, and one-facility-per-cell
// within each town.
func Validate(fs []Facility) error {
	seen := make(map[[3]int]bool, len(fs))
	for i, f := range fs {
		if f.Key == "" {
			return fmt.Errorf("facility %d: key is required", i)
		}
		if f.Img == "" {
			return fmt.Errorf("facility %d (%s): img is required", i, f.Key)
		}
		if f.Town < 0 || f.Town >= MaxTowns {
			return fmt.Errorf("facility %d (%s): town %d out of range 0..%d", i, f.Key, f.Town, MaxTowns-1)
		}
		if f.Col < 1 || f.Col > Cols {
			return fmt.Errorf("facility %d (%s): col %d out of range 1..%d", i, f.Key, f.Col, Cols)
		}
		if f.Row < 0 || f.Row >= Rows {
			return fmt.Errorf("facility %d (%s): row %d out of range 0..%d", i, f.Key, f.Row, Rows-1)
		}
		if f.Dest < 0 || f.Dest >= MaxTowns {
			return fmt.Errorf("facility %d (%s): dest %d out of range 0..%d", i, f.Key, f.Dest, MaxTowns-1)
		}
		cell := [3]int{f.Town, f.Col, f.Row}
		if seen[cell] {
			return fmt.Errorf("facility %d (%s): town %d cell (%d,%d) already occupied", i, f.Key, f.Town, f.Col, f.Row)
		}
		seen[cell] = true
	}
	return nil
}

// Set validates, persists, and applies a new map (used by the admin API).
func (s *Store) Set(ctx context.Context, fs []Facility) error {
	if err := Validate(fs); err != nil {
		return err
	}
	b, err := json.Marshal(fs)
	if err != nil {
		return fmt.Errorf("encode town map: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE town_map SET facilities = $1, updated_at = now() WHERE id = 1`, b); err != nil {
		return fmt.Errorf("save town map: %w", err)
	}
	s.mu.Lock()
	s.facilities = fs
	s.mu.Unlock()
	return nil
}

// MaxAssetLayers is how many background assets may stack on one cell.
// Array order is the z-order: later entries draw on top (e.g. tree over grass).
const MaxAssetLayers = 3

// ValidateAssets checks grid bounds, required img, and the per-cell layer cap
// within each town.
func ValidateAssets(as []Asset) error {
	count := make(map[[3]int]int, len(as))
	for i, a := range as {
		if a.Img == "" {
			return fmt.Errorf("asset %d: img is required", i)
		}
		if a.Town < 0 || a.Town >= MaxTowns {
			return fmt.Errorf("asset %d: town %d out of range 0..%d", i, a.Town, MaxTowns-1)
		}
		if a.Col < 1 || a.Col > Cols {
			return fmt.Errorf("asset %d: col %d out of range 1..%d", i, a.Col, Cols)
		}
		if a.Row < 0 || a.Row >= Rows {
			return fmt.Errorf("asset %d: row %d out of range 0..%d", i, a.Row, Rows-1)
		}
		cell := [3]int{a.Town, a.Col, a.Row}
		count[cell]++
		if count[cell] > MaxAssetLayers {
			return fmt.Errorf("asset %d: town %d cell (%d,%d) exceeds %d layers", i, a.Town, a.Col, a.Row, MaxAssetLayers)
		}
	}
	return nil
}

// SetAssets validates, persists, and applies a new background layer.
func (s *Store) SetAssets(ctx context.Context, as []Asset) error {
	if err := ValidateAssets(as); err != nil {
		return err
	}
	b, err := json.Marshal(as)
	if err != nil {
		return fmt.Errorf("encode town assets: %w", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE town_map SET assets = $1, updated_at = now() WHERE id = 1`, b); err != nil {
		return fmt.Errorf("save town assets: %w", err)
	}
	s.mu.Lock()
	s.assets = as
	s.mu.Unlock()
	return nil
}
