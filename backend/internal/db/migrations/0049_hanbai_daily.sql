-- +goose Up
-- 既存環境の app_settings に hanbai_daily_count が無ければ3(旧仕様の陳列数)を補う。
UPDATE app_settings
SET game = jsonb_set(game, '{hanbai_daily_count}', '3', true)
WHERE NOT (game ? 'hanbai_daily_count');

-- +goose Down
UPDATE app_settings SET game = game - 'hanbai_daily_count';
