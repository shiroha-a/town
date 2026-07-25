-- +goose Up
-- 建設会社(フェーズ2): プレイヤーが街の空地に建てた家。1マス1家(town,row,col でユニーク)。
-- interior_rank: 0=A(枠4)..3=D(枠1)。tuika: 0=家のみ/1=運営/2=株式会社/3=持ち物販売店。
CREATE TABLE player_houses (
    id            BIGSERIAL PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES players(id),
    town          INT NOT NULL,            -- 街番号(0=公園..4=謎の街)
    grid_row      INT NOT NULL,            -- グリッド行(0..Rows-1)
    grid_col      INT NOT NULL,            -- グリッド列(1..Cols)
    exterior      TEXT NOT NULL,           -- 外装画像キー(house4 等)
    interior_rank INT NOT NULL DEFAULT 0,  -- 内装ランク(0=A..3=D)
    tuika         INT NOT NULL DEFAULT 0,  -- 付帯種別(0家のみ/1運営/2株式会社/3持ち物店)
    built_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (town, grid_row, grid_col)
);
CREATE INDEX idx_player_houses_owner ON player_houses(owner_id);

-- +goose Down
DROP TABLE player_houses;
