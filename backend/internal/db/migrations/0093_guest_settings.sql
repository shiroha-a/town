-- +goose Up
-- お試しプレイ(ゲスト)の設定を既存環境にも入れる。JSONに無いと false / 0 になり、
-- 受け付けない・寿命0分として扱われてしまうため。
UPDATE app_settings
SET game = jsonb_set(game, '{guest_enabled}', 'true', true)
WHERE NOT (game ? 'guest_enabled');

UPDATE app_settings
SET game = jsonb_set(game, '{guest_lifetime_min}', '60', true)
WHERE NOT (game ? 'guest_lifetime_min');

-- +goose Down
UPDATE app_settings SET game = game - 'guest_enabled' - 'guest_lifetime_min';
