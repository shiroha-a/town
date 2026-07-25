-- +goose Up
-- 既存のtown_mapに自販機(hanbai)施設が無ければ追加する。
-- townmap.Default()はDBにレイアウトが保存済みだと上書きされるため、DB側に直接追加する。
UPDATE town_map
SET facilities = facilities || '[{"key":"hanbai","img":"hanbai","alt":"自動販売機","col":4,"row":4,"ready":true}]'::jsonb
WHERE id = 1 AND NOT (facilities @> '[{"key":"hanbai"}]');

-- +goose Down
UPDATE town_map
SET facilities = (SELECT jsonb_agg(f) FROM jsonb_array_elements(facilities) f WHERE f->>'key' != 'hanbai')
WHERE id = 1;
