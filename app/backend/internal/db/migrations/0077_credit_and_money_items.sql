-- +goose Up
-- (1) クレジット払いを有効にするアイテムをデータで表現する。
-- レガシーは商品データの効果列が「クレジット」かどうかで判定していた(depart.cgi:259 ほか)。
-- リライトはGo側に名前をハードコードしていたため、ブラタンメンバーズカードが漏れており、
-- 管理画面で新しいカードを追加しても機能しなかった。
ALTER TABLE content_items ADD COLUMN enables_credit BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE content_items SET enables_credit = TRUE
 WHERE name IN ('クレジットカード', 'ゴールドクレジットカード', 'スペシャルクレジットカード', 'ブラタンメンバーズカード');

-- (2) マネー系(換金)アイテム。レガシー basic0.cgi の $koukahadou eq "マネー" は
-- 使用時に単価(値段÷耐久)分の現金が手に入る。値段・耐久は固定なので、その額を
-- add_money として持たせる(釣りの餌 100/10=10円、小魚 1000/5=200円、中魚 1500/3=500円)。
UPDATE content_items SET effect = effect || '[{"op": "add_money", "amount": 10}]'::jsonb
 WHERE name = '釣りの餌(初)' AND NOT effect @> '[{"op": "add_money"}]'::jsonb;
UPDATE content_items SET effect = effect || '[{"op": "add_money", "amount": 200}]'::jsonb
 WHERE name = '小魚(小)' AND NOT effect @> '[{"op": "add_money"}]'::jsonb;
UPDATE content_items SET effect = effect || '[{"op": "add_money", "amount": 500}]'::jsonb
 WHERE name = '中魚(中)' AND NOT effect @> '[{"op": "add_money"}]'::jsonb;

-- +goose Down
UPDATE content_items SET effect = COALESCE((
  SELECT jsonb_agg(e) FROM jsonb_array_elements(effect) e
  WHERE e->>'op' <> 'add_money'
), '[]'::jsonb)
WHERE name IN ('釣りの餌(初)', '小魚(小)', '中魚(中)');
ALTER TABLE content_items DROP COLUMN enables_credit;
