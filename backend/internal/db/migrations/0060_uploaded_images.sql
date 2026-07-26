-- +goose Up
-- 管理者がアップロードした画像(背景アセット等)。DBに実体を持ち、バックエンドが
-- /api/v1/assets/{name} で配信する。nameはURL用のスラッグ。
CREATE TABLE uploaded_images (
    name       TEXT PRIMARY KEY,
    mime       TEXT NOT NULL,
    data       BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE uploaded_images;
