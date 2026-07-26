-- +goose Up
-- 建設会社(kentiku)施設を有効化する。townmap.Default()はDBにレイアウトが保存済みだと
-- 無視されるため、既存環境ではDBのfacilitiesを直接更新してready=trueにする。
UPDATE town_map
SET facilities = (
  SELECT jsonb_agg(
    CASE WHEN f->>'key' = 'kentiku' THEN jsonb_set(f, '{ready}', 'true') ELSE f END)
  FROM jsonb_array_elements(facilities) f)
WHERE id = 1;

-- +goose Down
UPDATE town_map
SET facilities = (
  SELECT jsonb_agg(
    CASE WHEN f->>'key' = 'kentiku' THEN jsonb_set(f, '{ready}', 'false') ELSE f END)
  FROM jsonb_array_elements(facilities) f)
WHERE id = 1;
