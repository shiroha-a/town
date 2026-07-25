-- +goose Up
-- ミニゲーム(カジノ)共通のプレイ履歴。game=ゲーム種別、bet=掛け金、payout=払戻
-- (掛け金返却分を含む)、detail=ゲーム別の結果詳細(JSON)。
CREATE TABLE game_plays (
    id         BIGSERIAL PRIMARY KEY,
    game       TEXT   NOT NULL,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    bet        BIGINT NOT NULL,
    payout     BIGINT NOT NULL,
    detail     JSONB  NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX game_plays_game_idx ON game_plays(game, id DESC);
CREATE INDEX game_plays_player_idx ON game_plays(player_id, id DESC);

-- +goose Down
DROP TABLE game_plays;
