-- +goose Up
-- 「使う」ができるかをアイテムごとに切り替えられるようにする。
--
-- 建築許可証・乗り物・クレジットカードのような、持っていること自体が意味を持つ品は
-- 使っても何も起きないのに耐久だけ減る(=消える)。使えないようにして誤操作で失うのを防ぐ。
ALTER TABLE content_items ADD COLUMN usable BOOLEAN NOT NULL DEFAULT TRUE;

-- 持ち物になる品(店・自販機)のうち、使っても何も起きないものを使用不可にする。
-- 効果が空で、食べてもカロリーが無いなら、使用は耐久を捨てるだけの操作になる。
--   建築許可証 / 乗り物11種 / クレジットカード4種 / 破魔矢
UPDATE content_items SET usable = FALSE
 WHERE facility IN ('', 'hanbai') AND effect = '[]'::jsonb AND calorie_g = 0;

-- +goose Down
ALTER TABLE content_items DROP COLUMN usable;
