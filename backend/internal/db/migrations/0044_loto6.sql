-- +goose Up
-- ロト6の購入券と抽選結果。全員共有のプールで、日次で抽選し銀行普通口座へ賞金を振り込む。
CREATE TABLE loto6_tickets (
    id         BIGSERIAL PRIMARY KEY,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    game_date  DATE   NOT NULL,      -- 購入日(この日の抽選対象)
    numbers    INT[]  NOT NULL,      -- 選んだ6個(1..36)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX loto6_tickets_date_idx ON loto6_tickets(game_date, player_id);

CREATE TABLE loto6_draws (
    game_date DATE PRIMARY KEY,
    winning   INT[] NOT NULL,        -- 当選6個
    drawn_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE loto6_tickets;
DROP TABLE loto6_draws;
