-- +goose Up
-- ビンゴ会場をタウンマップに追加する(0080/0082と同じ方式)。
UPDATE town_map
SET facilities = facilities || jsonb_build_array(
  jsonb_build_object('key', 'bingo', 'img', 'bingo', 'alt', 'ビンゴ会場', 'town', 0, 'col', 9, 'row', 9, 'dest', 0, 'ready', true)
)
WHERE id = 1
  AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(facilities) f WHERE f->>'key' = 'bingo');

-- +goose Down
UPDATE town_map
SET facilities = (
  SELECT COALESCE(jsonb_agg(f), '[]'::jsonb) FROM jsonb_array_elements(facilities) f
  WHERE f->>'key' <> 'bingo')
WHERE id = 1;
