-- +goose Up

-- アイテムの使用間隔(分)。レガシーのデパート表「間隔」列に相当。
-- 0=間隔なし。まずは表示のみ(実際の使用クールダウン制限は今後の改善)。
ALTER TABLE content_items ADD COLUMN use_interval_min INT NOT NULL DEFAULT 0;

UPDATE content_items SET use_interval_min = 30 WHERE name = '栄養ドリンク';
UPDATE content_items SET use_interval_min = 60 WHERE name = '参考書';

-- +goose Down
ALTER TABLE content_items DROP COLUMN use_interval_min;
