-- +goose Up
-- 街の一覧(名前・地価)を設定に追加。既存のapp_settingsに既定の5街を補填する
-- (未設定の場合のみ。既に'towns'があれば保持)。
UPDATE app_settings
SET game = jsonb_build_object('towns', '[
  {"name":"公園","land_price":2000},
  {"name":"シー・リゾート","land_price":1000},
  {"name":"カントリータウン","land_price":500},
  {"name":"ダウンタウン","land_price":250},
  {"name":"謎の街","land_price":250}
]'::jsonb) || game
WHERE id = 1;

-- +goose Down
UPDATE app_settings SET game = game - 'towns' WHERE id = 1;
