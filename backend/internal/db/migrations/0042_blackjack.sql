-- +goose Up
-- ブラックジャックの進行状態。1プレイヤーにつき1ゲーム(進行中/決着)を保持する。
-- oya/plyは0..51のカード配列。phase=playing(進行中)/over(決着)。
CREATE TABLE player_blackjack (
    player_id  BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    rate       BIGINT NOT NULL,
    oya        INT[]  NOT NULL,
    ply        INT[]  NOT NULL,
    phase      TEXT   NOT NULL,        -- 'playing' | 'over'
    result     TEXT   NOT NULL DEFAULT '', -- 'win' | 'lose' | 'push'
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE player_blackjack;
