-- +goose Up
-- 既存のapp_settingsにitem_kind_limitが無ければ25(旧TOWN 25品目)を補う。
-- 新規環境はdefault.ymlのseedで25が入るが、既存環境は旧JSONに無く0(無制限)になるため。
UPDATE app_settings
SET game = jsonb_set(game, '{item_kind_limit}', '25', true)
WHERE NOT (game ? 'item_kind_limit');

-- +goose Down
UPDATE app_settings SET game = game - 'item_kind_limit';
