-- +goose Up
-- 釣りゲームとギフト屋をタウンマップに追加する。townmap.Default()はDBにレイアウトが
-- 保存済みだと使われないため、既存環境ではfacilitiesへ直接追記する(0052と同じ方式)。
-- 置き場所は公園(town 0)の空いているマス。管理画面から移動できる。
UPDATE town_map
SET facilities = facilities || jsonb_build_array(
  jsonb_build_object('key', 'tsuri',    'img', 'tsuri',    'alt', '釣りゲーム', 'town', 0, 'col', 2,  'row', 9, 'dest', 0, 'ready', true),
  jsonb_build_object('key', 'gifutoya', 'img', 'gifutoya', 'alt', 'ギフト屋',   'town', 0, 'col', 12, 'row', 9, 'dest', 0, 'ready', true)
)
WHERE id = 1
  AND NOT EXISTS (
    SELECT 1 FROM jsonb_array_elements(facilities) f WHERE f->>'key' = 'tsuri'
  );

-- +goose Down
UPDATE town_map
SET facilities = (
  SELECT COALESCE(jsonb_agg(f), '[]'::jsonb) FROM jsonb_array_elements(facilities) f
  WHERE f->>'key' NOT IN ('tsuri', 'gifutoya'))
WHERE id = 1;
