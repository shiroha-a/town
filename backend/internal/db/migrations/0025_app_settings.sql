-- +goose Up
-- 管理者が実行時に編集できるゲーム設定(単一行JSONB)。初回起動時にdefault.ymlから
-- settings.NewStore がシードする。
CREATE TABLE app_settings (
    id         INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    game       JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE app_settings;
