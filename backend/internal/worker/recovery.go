package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RecoverPower advances every player's 身体パワー / 頭脳パワー based on elapsed
// time. It grants floor(elapsed / rate) points, caps at the per-player max, and
// advances the recovery timestamp by exactly the granted time so no recovery is
// lost (and the timestamp never exceeds now()). Returns the rows updated.
func RecoverPower(ctx context.Context, pool *pgxpool.Pool, energySec, nouSec int) (int64, error) {
	if energySec <= 0 {
		energySec = 60
	}
	if nouSec <= 0 {
		nouSec = 60
	}
	// 入浴中(onsen_multiplier>1)は実効回復秒を1/倍率にして回復を加速する。
	// 付与後に両パワーが満タンになったら入浴を終了し倍率を1(通常)へ戻す。
	tag, err := pool.Exec(ctx, `
		UPDATE player_status ps SET
			energy = LEAST(ps.energy_max, ps.energy + g.gain_e),
			energy_recovered_at = ps.energy_recovered_at + make_interval(secs => (g.gain_e * $1::double precision / ps.onsen_multiplier)),
			nou_energy = LEAST(ps.nou_energy_max, ps.nou_energy + g.gain_n),
			nou_recovered_at = ps.nou_recovered_at + make_interval(secs => (g.gain_n * $2::double precision / ps.onsen_multiplier)),
			onsen_multiplier = CASE
				WHEN LEAST(ps.energy_max, ps.energy + g.gain_e) >= ps.energy_max
				 AND LEAST(ps.nou_energy_max, ps.nou_energy + g.gain_n) >= ps.nou_energy_max
				THEN 1 ELSE ps.onsen_multiplier END
		FROM (
			SELECT player_id,
				FLOOR(EXTRACT(EPOCH FROM (now() - energy_recovered_at)) / ($1::double precision / onsen_multiplier))::int AS gain_e,
				FLOOR(EXTRACT(EPOCH FROM (now() - nou_recovered_at)) / ($2::double precision / onsen_multiplier))::int AS gain_n
			FROM player_status
		) g
		WHERE ps.player_id = g.player_id AND (g.gain_e > 0 OR g.gain_n > 0)
	`, energySec, nouSec)
	if err != nil {
		return 0, fmt.Errorf("recover power: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DecaySatiety reduces every player's 満腹度(空腹値) over time by
// floor(elapsed / decaySec) points, flooring at 0 and advancing the timestamp
// so no decay is lost. Returns the rows updated.
func DecaySatiety(ctx context.Context, pool *pgxpool.Pool, decaySec int) (int64, error) {
	if decaySec <= 0 {
		decaySec = 300
	}
	tag, err := pool.Exec(ctx, `
		UPDATE player_status ps SET
			satiety = GREATEST(0, ps.satiety - g.dec),
			satiety_updated_at = ps.satiety_updated_at + make_interval(secs => (g.dec * $1)::double precision)
		FROM (
			SELECT player_id,
				FLOOR(EXTRACT(EPOCH FROM (now() - satiety_updated_at)) / $1)::int AS dec
			FROM player_status
		) g
		WHERE ps.player_id = g.player_id AND g.dec > 0
	`, decaySec)
	if err != nil {
		return 0, fmt.Errorf("decay satiety: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DecayDayItems reduces the remaining days of 'day'-unit durability items by one
// and deletes any that have expired. Runs once per game day inside the daily job
// transaction so it is idempotent per game date.
func DecayDayItems(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx,
		`UPDATE player_items pi SET remaining_uses = pi.remaining_uses - 1, updated_at = now()
		 FROM content_items ci
		 WHERE pi.item_id = ci.id AND ci.durability_unit = 'day' AND pi.remaining_uses > 0`); err != nil {
		return fmt.Errorf("decay day items: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM player_items pi USING content_items ci
		 WHERE pi.item_id = ci.id AND ci.durability_unit = 'day' AND pi.remaining_uses <= 0`); err != nil {
		return fmt.Errorf("delete expired items: %w", err)
	}
	return nil
}
