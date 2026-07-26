-- +goose Up

-- アイテムの使用クールダウン用に最終使用時刻を持つ。
-- 使用間隔(content_items.use_interval_min)内は同じアイテムを再使用できない。
ALTER TABLE player_items ADD COLUMN last_used_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE player_items DROP COLUMN last_used_at;
