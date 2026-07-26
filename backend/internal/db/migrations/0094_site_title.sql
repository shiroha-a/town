-- +goose Up
-- ゲーム名と副題を設定へ。既存環境のJSONには無く、空だと画面の見出しが
-- 消えてしまうため、これまで画面に直書きしていた値を入れる。
UPDATE app_settings
SET game = jsonb_set(game, '{site_title}', '"ＴＯＷＮ"', true)
WHERE NOT (game ? 'site_title');

UPDATE app_settings
SET game = jsonb_set(game, '{site_tagline}', '"働いて、買って、暮らす街"', true)
WHERE NOT (game ? 'site_tagline');

-- +goose Down
UPDATE app_settings SET game = game - 'site_title' - 'site_tagline';
