package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shiroha-a/town/internal/condition"
	"github.com/shiroha-a/town/internal/effects"
)

// 病気指数の上下限は効果エンジンと共有する(アイテムの回復効果と日次ドリフトで
// 範囲が食い違わないようにするため)。
const (
	diseaseCeil  = effects.DiseaseCeil
	diseaseFloor = effects.DiseaseFloor
)

// EvaluateDisease advances the disease index of every player whose last
// evaluation is at least intervalMin minutes old (design 17.4). Each due player
// gets exactly one evaluation: the index moves by the delta for their current
// base condition, is clamped to [diseaseFloor, diseaseCeil], and
// disease_evaled_at is set to now. Rows are read fully before any write so the
// query cursor and the updates never contend for the same pooled connection.
// Returns the number of players updated.
func EvaluateDisease(ctx context.Context, pool *pgxpool.Pool, intervalMin int) (int, error) {
	if intervalMin <= 0 {
		intervalMin = 10
	}
	rows, err := pool.Query(ctx, `
		SELECT player_id, energy, energy_max, nou_energy, nou_energy_max,
		       kenkou, satiety, height_cm, weight_g, disease_index
		FROM player_status
		WHERE disease_evaled_at <= now() - make_interval(mins => $1)`, intervalMin)
	if err != nil {
		return 0, fmt.Errorf("select due players: %w", err)
	}
	type due struct {
		id       int64
		newIndex int
	}
	var updates []due
	for rows.Next() {
		var (
			id                             int64
			energy, energyMax, nou, nouMax int
			kenkou, satiety                int
			heightCm, weightG, index       int
		)
		if err := rows.Scan(&id, &energy, &energyMax, &nou, &nouMax,
			&kenkou, &satiety, &heightCm, &weightG, &index); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan due player: %w", err)
		}
		cond := condition.Compute(condition.Input{
			Energy: energy, EnergyMax: energyMax,
			NouEnergy: nou, NouEnergyMax: nouMax,
			Kenkou: kenkou, Satiety: satiety,
			BMI:          condition.BMI(heightCm, weightG),
			DiseaseIndex: index,
		})
		next := index + condition.DiseaseDelta(cond.Label)
		if next > diseaseCeil {
			next = diseaseCeil
		}
		if next < diseaseFloor {
			next = diseaseFloor
		}
		updates = append(updates, due{id: id, newIndex: next})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("iterate due players: %w", err)
	}
	rows.Close()

	for _, u := range updates {
		if _, err := pool.Exec(ctx,
			`UPDATE player_status SET disease_index = $1, disease_evaled_at = now(), updated_at = now()
			 WHERE player_id = $2`, u.newIndex, u.id); err != nil {
			return 0, fmt.Errorf("update disease: %w", err)
		}
	}
	return len(updates), nil
}
