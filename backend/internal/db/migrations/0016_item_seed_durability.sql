-- +goose Up

-- 既存アイテムにレガシー準拠の耐久・在庫・カロリーを付与。
-- デパート販売品(facility=''): 在庫と耐久を設定。
UPDATE content_items SET durability = 1, stock_master = 20                  WHERE id = 1; -- 栄養ドリンク(1回)
UPDATE content_items SET durability = 6, stock_master = 15                  WHERE id = 2; -- 参考書(6回)
UPDATE content_items SET durability = 1, stock_master = 10, calorie_g = 500 WHERE id = 3; -- 高級弁当(1回)

-- 食堂メニュー(facility='syokudou'): 在庫とカロリーを設定(食事は1回)。
UPDATE content_items SET stock_master = 6,  calorie_g = 800  WHERE id = 4; -- スッポン
UPDATE content_items SET stock_master = 12, calorie_g = 900  WHERE id = 5; -- 中華丼
UPDATE content_items SET stock_master = 18, calorie_g = 1000 WHERE id = 6; -- カレー
UPDATE content_items SET stock_master = 20, calorie_g = 700  WHERE id = 7; -- オムライス

-- ジム/温泉(facility='gym'/'onsen')はサービスのため在庫・耐久の対象外(stock_master=NULL のまま)。

-- 日単位耐久のサンプル: フィットネス会員証(7日間有効、使うと運動効果、日数経過で失効)。
INSERT INTO content_items
    (name, category, facility, price, effect, use_interval_min, durability, durability_unit, stock_master)
VALUES
    ('フィットネス会員証', 'チケット', '', 5000,
     '[{"op":"add_param","param":"tairyoku","amount":1},{"op":"add_param","param":"kenkou","amount":1}]'::jsonb,
     10, 7, 'day', 10);

-- +goose Down
DELETE FROM content_items WHERE name = 'フィットネス会員証';
UPDATE content_items SET durability = 1, durability_unit = 'use', stock_master = NULL, calorie_g = 0
WHERE id BETWEEN 1 AND 7;
