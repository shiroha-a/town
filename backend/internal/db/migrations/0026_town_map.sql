-- +goose Up
-- 管理者が実行時に編集できる街マップ(施設配置の単一行JSONB)。初回起動時に
-- townmap.NewStore が既定の施設配置をシードする。
CREATE TABLE town_map (
    id         INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    facilities JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE town_map;
