-- +goose Up
-- タイムゾーンと日付の切り替わり時刻を設定ファイルからDBへ移した(管理画面で編集する)。
-- 既存環境のJSONには無いため、これまで default.yml で使っていた値を補う。
-- 補わないと timezone="" (=UTC) / day_boundary_hour=0 になり、日次処理の時刻がずれる。
UPDATE app_settings
SET game = jsonb_set(game, '{timezone}', '"Asia/Tokyo"', true)
WHERE NOT (game ? 'timezone');

UPDATE app_settings
SET game = jsonb_set(game, '{day_boundary_hour}', '5', true)
WHERE NOT (game ? 'day_boundary_hour');

-- +goose Down
UPDATE app_settings SET game = game - 'timezone' - 'day_boundary_hour';
