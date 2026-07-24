package content

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// EventCond is one eligibility condition of an AdminEvent: 条件をすべて満たす
// プレイヤーにだけそのイベントが発生する。
type EventCond struct {
	Pred   string `json:"pred"` // money_gte/money_lte/param_gte/param_lte/has_item/job_is
	Param  string `json:"param,omitempty"`
	Value  int64  `json:"value,omitempty"`
	ItemID int64  `json:"item_id,omitempty"`
	Job    string `json:"job,omitempty"`
}

// eventCondParams are the parameter keys usable in param_gte/param_lte.
var eventCondParams = map[string]bool{
	"kokugo": true, "suugaku": true, "rika": true, "syakai": true, "eigo": true,
	"ongaku": true, "bijutsu": true, "looks": true, "tairyoku": true, "kenkou": true,
	"speed": true, "power": true, "wanryoku": true, "kyakuryoku": true, "love": true,
	"omoshirosa": true, "energy": true, "nou_energy": true, "satiety": true,
}

// AdminEvent is an admin-defined random event (content_events)。発生時は
// 金額[money_min, money_max]の一様乱数、params(パラメータ増減)、
// disease_set(病気指数の直接代入)、weight_g(体重増減)が適用される。
// Conditionsを満たすプレイヤーにだけ抽選候補になる。
type AdminEvent struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	Message    string         `json:"message"`
	Good       bool           `json:"good"`
	MoneyMin   int64          `json:"money_min"`
	MoneyMax   int64          `json:"money_max"`
	Params     map[string]int `json:"params"`
	DiseaseSet *int           `json:"disease_set"`
	WeightG    int            `json:"weight_g"`
	Weight     int            `json:"weight"` // 抽選の重み(組み込みは各1)
	Enabled    bool           `json:"enabled"`
	Conditions []EventCond    `json:"conditions"`
}

// validAdminEvent checks the editable fields.
func validAdminEvent(e AdminEvent) error {
	if e.Name == "" {
		return errors.New("name is required")
	}
	if e.Message == "" {
		return errors.New("message is required")
	}
	if e.MoneyMax < e.MoneyMin {
		return errors.New("money_max must be >= money_min")
	}
	if e.Weight < 1 || e.Weight > 100 {
		return errors.New("weight must be 1..100")
	}
	for i, c := range e.Conditions {
		switch c.Pred {
		case "money_gte", "money_lte":
		case "param_gte", "param_lte":
			if !eventCondParams[c.Param] {
				return fmt.Errorf("conditions[%d]: unknown param %q", i, c.Param)
			}
		case "has_item":
			if c.ItemID <= 0 {
				return fmt.Errorf("conditions[%d]: item_id is required", i)
			}
		case "job_is":
			if c.Job == "" {
				return fmt.Errorf("conditions[%d]: job is required", i)
			}
		default:
			return fmt.Errorf("conditions[%d]: unknown pred %q", i, c.Pred)
		}
	}
	return nil
}

const adminEventCols = `id, name, message, good, money_min, money_max, params, disease_set, weight_g, weight, enabled, conditions`

func scanAdminEvent(row pgx.Row) (AdminEvent, error) {
	var e AdminEvent
	err := row.Scan(&e.ID, &e.Name, &e.Message, &e.Good, &e.MoneyMin, &e.MoneyMax,
		&e.Params, &e.DiseaseSet, &e.WeightG, &e.Weight, &e.Enabled, &e.Conditions)
	if e.Conditions == nil {
		e.Conditions = []EventCond{}
	}
	return e, err
}

// ListAdminEvents returns every custom event (admin list, disabled included).
func (s *Service) ListAdminEvents(ctx context.Context) ([]AdminEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+adminEventCols+` FROM content_events ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	out := []AdminEvent{}
	for rows.Next() {
		e, err := scanAdminEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateAdminEvent inserts a custom event.
func (s *Service) CreateAdminEvent(ctx context.Context, e AdminEvent) (AdminEvent, error) {
	if err := validAdminEvent(e); err != nil {
		return AdminEvent{}, err
	}
	if e.Params == nil {
		e.Params = map[string]int{}
	}
	if e.Conditions == nil {
		e.Conditions = []EventCond{}
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO content_events (name, message, good, money_min, money_max, params, disease_set, weight_g, weight, enabled, conditions)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING `+adminEventCols,
		e.Name, e.Message, e.Good, e.MoneyMin, e.MoneyMax, e.Params, e.DiseaseSet, e.WeightG, e.Weight, e.Enabled, e.Conditions)
	out, err := scanAdminEvent(row)
	if err != nil {
		return AdminEvent{}, fmt.Errorf("create event: %w", err)
	}
	return out, nil
}

// UpdateAdminEvent replaces a custom event's editable fields.
func (s *Service) UpdateAdminEvent(ctx context.Context, e AdminEvent) (AdminEvent, error) {
	if err := validAdminEvent(e); err != nil {
		return AdminEvent{}, err
	}
	if e.Params == nil {
		e.Params = map[string]int{}
	}
	if e.Conditions == nil {
		e.Conditions = []EventCond{}
	}
	row := s.pool.QueryRow(ctx,
		`UPDATE content_events SET name = $2, message = $3, good = $4, money_min = $5, money_max = $6,
		   params = $7, disease_set = $8, weight_g = $9, weight = $10, enabled = $11, conditions = $12
		 WHERE id = $1 RETURNING `+adminEventCols,
		e.ID, e.Name, e.Message, e.Good, e.MoneyMin, e.MoneyMax, e.Params, e.DiseaseSet, e.WeightG, e.Weight, e.Enabled, e.Conditions)
	out, err := scanAdminEvent(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminEvent{}, errors.New("event not found")
	}
	if err != nil {
		return AdminEvent{}, fmt.Errorf("update event: %w", err)
	}
	return out, nil
}

// DeleteAdminEvent removes a custom event.
func (s *Service) DeleteAdminEvent(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM content_events WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("event not found")
	}
	return nil
}
