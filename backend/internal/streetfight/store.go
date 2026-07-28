package streetfight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service reads and edits the monster master.
type Service struct {
	pool *pgxpool.Pool
}

// New builds the service.
func New(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// ValidationError is a bad admin input (mapped to 422 by the HTTP layer).
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

const monsterColumns = `m.id, m.name, m.level, m.win_money, m.lose_money, m.params,
	m.item_rate, m.reward_item_id, COALESCE(ci.name, ''), m.icon, m.enabled`

func scanMonster(row pgx.Row) (Monster, error) {
	var m Monster
	var params []byte
	if err := row.Scan(&m.ID, &m.Name, &m.Level, &m.WinMoney, &m.LoseMoney, &params,
		&m.ItemRate, &m.RewardItemID, &m.RewardName, &m.Icon, &m.Enabled); err != nil {
		return Monster{}, err
	}
	m.Params = map[string]int{}
	_ = json.Unmarshal(params, &m.Params)
	for _, k := range Abilities {
		if _, ok := m.Params[k]; !ok {
			m.Params[k] = 0
		}
	}
	return m, nil
}

// List returns every monster (admin listing).
func (s *Service) List(ctx context.Context) ([]Monster, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+monsterColumns+` FROM battle_monsters m
		 LEFT JOIN content_items ci ON ci.id = m.reward_item_id
		 ORDER BY m.level, m.id`)
	if err != nil {
		return nil, fmt.Errorf("list monsters: %w", err)
	}
	defer rows.Close()
	out := []Monster{}
	for rows.Next() {
		m, err := scanMonster(rows)
		if err != nil {
			return nil, fmt.Errorf("scan monster: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ErrNoMonsters means no monster is enabled, so nobody is out on the street.
var ErrNoMonsters = errors.New("no monster")

// PickRandom returns one enabled monster at random, inside the caller's tx so the
// draw and the fight share a transaction.
func PickRandom(ctx context.Context, tx pgx.Tx) (Monster, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+monsterColumns+` FROM battle_monsters m
		 LEFT JOIN content_items ci ON ci.id = m.reward_item_id
		 WHERE m.enabled ORDER BY random() LIMIT 1`)
	m, err := scanMonster(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Monster{}, ErrNoMonsters
	}
	if err != nil {
		return Monster{}, fmt.Errorf("pick monster: %w", err)
	}
	return m, nil
}

// Input is the admin-editable shape of a monster.
type Input struct {
	Name         string         `json:"name"`
	Level        int            `json:"level"`
	WinMoney     int64          `json:"win_money"`
	LoseMoney    int64          `json:"lose_money"`
	Params       map[string]int `json:"params"`
	ItemRate     int            `json:"item_rate"`
	RewardItemID *int64         `json:"reward_item_id"`
	Icon         string         `json:"icon"`
	Enabled      bool           `json:"enabled"`
}

// validate rejects admin input that would break a fight.
func (in *Input) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return &ValidationError{Message: "名前を入力してください。"}
	}
	if in.Level < 1 {
		in.Level = 1
	}
	if in.WinMoney < 0 || in.LoseMoney < 0 {
		return &ValidationError{Message: "金額は0以上にしてください。"}
	}
	if in.ItemRate < 0 {
		return &ValidationError{Message: "景品の当たりやすさは0以上にしてください。"}
	}
	if in.Icon == "" {
		in.Icon = "slime"
	}
	clean := map[string]int{}
	for _, k := range Abilities {
		v := in.Params[k]
		if v < 0 {
			return &ValidationError{Message: "能力は0以上にしてください。"}
		}
		clean[k] = v
	}
	// 未知のキーは捨てる(戦闘は Abilities しか見ない)。
	in.Params = clean
	return nil
}

// Create inserts a monster.
func (s *Service) Create(ctx context.Context, in Input) (Monster, error) {
	if err := in.validate(); err != nil {
		return Monster{}, err
	}
	params, _ := json.Marshal(in.Params)
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO battle_monsters (name, level, win_money, lose_money, params, item_rate,
		                              reward_item_id, icon, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		in.Name, in.Level, in.WinMoney, in.LoseMoney, params, in.ItemRate,
		in.RewardItemID, in.Icon, in.Enabled).Scan(&id); err != nil {
		return Monster{}, fmt.Errorf("create monster: %w", err)
	}
	return s.Get(ctx, id)
}

// Update replaces a monster.
func (s *Service) Update(ctx context.Context, id int64, in Input) (Monster, error) {
	if err := in.validate(); err != nil {
		return Monster{}, err
	}
	params, _ := json.Marshal(in.Params)
	tag, err := s.pool.Exec(ctx,
		`UPDATE battle_monsters SET name = $2, level = $3, win_money = $4, lose_money = $5,
		        params = $6, item_rate = $7, reward_item_id = $8, icon = $9, enabled = $10,
		        updated_at = now()
		 WHERE id = $1`,
		id, in.Name, in.Level, in.WinMoney, in.LoseMoney, params, in.ItemRate,
		in.RewardItemID, in.Icon, in.Enabled)
	if err != nil {
		return Monster{}, fmt.Errorf("update monster: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return Monster{}, &ValidationError{Message: "そのモンスターはありません。"}
	}
	return s.Get(ctx, id)
}

// Get returns one monster.
func (s *Service) Get(ctx context.Context, id int64) (Monster, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+monsterColumns+` FROM battle_monsters m
		 LEFT JOIN content_items ci ON ci.id = m.reward_item_id
		 WHERE m.id = $1`, id)
	m, err := scanMonster(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Monster{}, &ValidationError{Message: "そのモンスターはありません。"}
	}
	if err != nil {
		return Monster{}, fmt.Errorf("get monster: %w", err)
	}
	return m, nil
}

// Delete removes a monster.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM battle_monsters WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete monster: %w", err)
	}
	return nil
}
