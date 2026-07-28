-- +goose Up
-- 「ランダム品」の効果をランダムに戻す。
--
-- レガシーは商品目が「ランダム品」の品を使うと、各パラメータの上昇値が0〜設定値の
-- 一様乱数になった(basic0.cgi:499 の int(rand($v + 1)))。リライトは移行時に設定値を
-- そのまま add_param に入れたので、使うたびに必ず最大値(+10)が入っていた。
-- 効果opの random フラグで表現する。
UPDATE content_items
SET effect = (
  SELECT jsonb_agg(
    CASE WHEN op->>'op' = 'add_param' THEN op || '{"random": true}'::jsonb ELSE op END
    ORDER BY ord
  )
  FROM jsonb_array_elements(effect) WITH ORDINALITY AS t(op, ord)
)
WHERE name = 'ランダム品' AND effect @> '[{"op": "add_param"}]'::jsonb;

-- +goose Down
UPDATE content_items
SET effect = (
  SELECT jsonb_agg((op - 'random') ORDER BY ord)
  FROM jsonb_array_elements(effect) WITH ORDINALITY AS t(op, ord)
)
WHERE name = 'ランダム品';
