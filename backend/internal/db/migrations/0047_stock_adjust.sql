-- +goose Up
-- 既存のapp_settingsにstock_adjustが無ければ2(旧zaiko_tyousetuti)を補う。
UPDATE app_settings
SET game = jsonb_set(game, '{stock_adjust}', '2', true)
WHERE NOT (game ? 'stock_adjust');

-- +goose Down
UPDATE app_settings SET game = game - 'stock_adjust';
