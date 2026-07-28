-- +goose Up
-- 持ち物の個数(quantity)を残量(remaining_uses)から数え直す。
--
-- 差し押さえイベントが個数だけを減らして残量を放置していたため、まだ持っているのに
-- 0個という行ができていた。街トップの所有物欄がここを見るので「○クレジットカード(0個)」
-- と出る。個数は「残量 ÷ 1セット耐久」で一意に決まるので、ずれている行を直す。
UPDATE player_items pi
SET quantity = CEIL(pi.remaining_uses::numeric / GREATEST(ci.durability, 1)),
    updated_at = now()
FROM content_items ci
WHERE ci.id = pi.item_id
  AND pi.quantity <> CEIL(pi.remaining_uses::numeric / GREATEST(ci.durability, 1));

-- +goose Down
-- 数え直す前の個数は残していないので戻せない。
SELECT 1;
