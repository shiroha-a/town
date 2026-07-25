-- +goose Up
-- 背景アセット配置レイヤー。施設(facilities)とは別に、装飾用の背景画像を
-- セル単位で置くための層。既存行にはデフォルトの空配列が入る。
ALTER TABLE town_map ADD COLUMN assets JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE town_map DROP COLUMN assets;
