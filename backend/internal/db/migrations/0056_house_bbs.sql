-- +goose Up
-- 家の掲示板(フェーズ3b): 通常掲示板(誰でも書ける)と家主板(家主のみ書ける)。
CREATE TABLE house_bbs (
    id          BIGSERIAL PRIMARY KEY,
    house_id    BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,                       -- 'normal'(通常掲示板) / 'nushi'(家主板)
    author_id   BIGINT REFERENCES players(id) ON DELETE SET NULL,
    author_name TEXT NOT NULL,
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_house_bbs ON house_bbs(house_id, kind, created_at DESC);

-- +goose Down
DROP TABLE house_bbs;
