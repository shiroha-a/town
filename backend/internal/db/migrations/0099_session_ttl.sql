-- +goose Up
-- ログインの有効期限を設定へ。これまでコードの定数(30日)だった。
-- 既存環境のJSONには無く、0のまま読まれると発行した瞬間に切れてしまうため、
-- これまでと同じ30日を入れる。
UPDATE app_settings
SET game = jsonb_set(game, '{session_ttl_days}', '30', true)
WHERE NOT (game ? 'session_ttl_days');

-- +goose Down
UPDATE app_settings SET game = game - 'session_ttl_days';
