-- +goose Up
-- 参加者表示(レガシーguestfile)用の最終アクセス時刻。ステータス取得のたびに
-- 更新され、20分($logout_time=1200)以内のプレイヤーを「現在の総参加者」に出す。
ALTER TABLE players ADD COLUMN last_seen_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE players DROP COLUMN last_seen_at;
