-- +goose Up
-- (1) マネー(換金)アイテムの金額を訂正する。
-- 0077では「値段÷耐久」で計算したが、レガシー basic0.cgi は商品データの単価欄が
-- 設定されていればそれをそのまま使う(`if(!$tanka && $syo_taikyuu){…}else{$syo_tanka = $tanka;}`)。
-- 商品データの単価は 餌=10 / 小魚=100 / 中魚=150 なので、小魚と中魚を訂正する。
UPDATE content_items SET effect = '[{"op": "add_money", "amount": 100}]'::jsonb WHERE name = '小魚(小)';
UPDATE content_items SET effect = '[{"op": "add_money", "amount": 150}]'::jsonb WHERE name = '中魚(中)';

-- (2) 釣りの景品(dat_dir/tsuri.cgi)のうちリライトに無かった3種を追加する。
-- 店売りはせず(enabled=false)、釣りでのみ入手できる。耐久は1回、使うと単価分の現金になる。
INSERT INTO content_items (name, category, price, effect, durability, durability_unit, enabled, stock_master)
SELECT v.name, '釣り', v.price, v.effect::jsonb, 1, 'use', FALSE, NULL
FROM (VALUES
    ('大魚(大)', 2000,  '[{"op": "add_money", "amount": 200}]'),
    ('特魚(特)', 2000,  '[{"op": "add_money", "amount": 2000}]'),
    ('幻魚(幻)', 20000, '[{"op": "add_money", "amount": 20000}]')
) AS v(name, price, effect)
WHERE NOT EXISTS (SELECT 1 FROM content_items ci WHERE ci.name = v.name);

-- (3) 贈答専用アイテム(レガシーの効果列「ギフト」)。購入すると持ち物ではなくギフト箱へ入る。
ALTER TABLE content_items ADD COLUMN is_gift BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE content_items SET is_gift = TRUE WHERE name LIKE '%(ギフト)%';

-- +goose Down
ALTER TABLE content_items DROP COLUMN is_gift;
DELETE FROM content_items WHERE name IN ('大魚(大)', '特魚(特)', '幻魚(幻)');
UPDATE content_items SET effect = '[{"op": "add_money", "amount": 200}]'::jsonb WHERE name = '小魚(小)';
UPDATE content_items SET effect = '[{"op": "add_money", "amount": 500}]'::jsonb WHERE name = '中魚(中)';
