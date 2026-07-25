-- +goose Up
-- prof(プロフィール)を「準備中」から解除する。0082等と同じ方式で、DBに保存済みの
-- レイアウトがあると townmap.Default() の変更は反映されないため直接書き換える。
UPDATE town_map
SET facilities = (
  SELECT COALESCE(jsonb_agg(
    CASE WHEN f ->> 'key' = 'prof' THEN f || '{"ready": true}'::jsonb ELSE f END), '[]'::jsonb)
  FROM jsonb_array_elements(facilities) f)
WHERE id = 1;

-- +goose Down
UPDATE town_map
SET facilities = (
  SELECT COALESCE(jsonb_agg(
    CASE WHEN f ->> 'key' = 'prof' THEN f || '{"ready": false}'::jsonb ELSE f END), '[]'::jsonb)
  FROM jsonb_array_elements(facilities) f)
WHERE id = 1;
