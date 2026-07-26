-- +goose Up
-- 街移動時間(徒歩/バス)を設定に追加。既存のapp_settingsに既定値を補填する
-- (未設定のキーのみ。既に値があれば保持)。
UPDATE app_settings
SET game = jsonb_build_object('move_walk_secs', 10, 'move_bus_secs', 5) || game
WHERE id = 1;

-- +goose Down
UPDATE app_settings
SET game = game - 'move_walk_secs' - 'move_bus_secs'
WHERE id = 1;
