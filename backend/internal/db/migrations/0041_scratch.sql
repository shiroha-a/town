-- +goose Up
-- スクラッチ(scratch/sukuratti)の日次カード状態。1日ぶんの5枚を保持し、セルの開封
-- 状態(opened)を更新していく。cellsは1..Nの順列(値<=閾値が当たり)。
CREATE TABLE player_scratch_cards (
    id         BIGSERIAL PRIMARY KEY,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    game       TEXT   NOT NULL,          -- 'scratch' | 'sukuratti'
    game_date  DATE   NOT NULL,          -- カード発行日(ゲーム日)
    card_index INT    NOT NULL,          -- 0..4(1日5枚)
    cells      INT[]  NOT NULL,          -- 1..Nの順列
    opened     INT[]  NOT NULL DEFAULT '{}', -- 開封済みセルのindex
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (player_id, game, game_date, card_index)
);

-- +goose Down
DROP TABLE player_scratch_cards;
