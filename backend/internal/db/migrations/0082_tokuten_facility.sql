-- +goose Up
-- 特典交換所をタウンマップに追加する(0080と同じ方式。DBに保存済みのレイアウトが
-- あると townmap.Default() は使われないため facilities へ直接追記する)。
UPDATE town_map
SET facilities = facilities || jsonb_build_array(
  jsonb_build_object('key', 'tokuten', 'img', 'tokuten', 'alt', '特典交換所', 'town', 0, 'col', 6, 'row', 9, 'dest', 0, 'ready', true)
)
WHERE id = 1
  AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(facilities) f WHERE f->>'key' = 'tokuten');

-- +goose Down
UPDATE town_map
SET facilities = (
  SELECT COALESCE(jsonb_agg(f), '[]'::jsonb) FROM jsonb_array_elements(facilities) f
  WHERE f->>'key' <> 'tokuten')
WHERE id = 1;
