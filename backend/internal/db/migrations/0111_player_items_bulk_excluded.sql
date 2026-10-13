-- +goose Up
-- 持ち物の一括使用から外す印(目安箱#14)。既定は外さない=チェックあり。
-- 行は使い切ると消えるので、買い直した品はまたチェックありから始まる。
ALTER TABLE player_items ADD COLUMN bulk_excluded BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE player_items DROP COLUMN bulk_excluded;
