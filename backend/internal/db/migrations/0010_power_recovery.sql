-- +goose Up

-- 身体/頭脳パワーの自動回復用に、最後に回復基準とした時刻を持つ。
-- workerが定期的に (経過秒 / 回復レート) 分だけ回復させ、この時刻を進める。
ALTER TABLE player_status
    ADD COLUMN energy_recovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN nou_recovered_at    TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE player_status
    DROP COLUMN energy_recovered_at,
    DROP COLUMN nou_recovered_at;
