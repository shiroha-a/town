-- +goose Up
-- おみくじの超大吉(0.1%)で授かる「破魔矢」。厄除けのお守り(所持するコレクションで効果は持たない)。
INSERT INTO content_items (name, category, price, effect, durability)
VALUES ('破魔矢', 'お守り', 0, '[]', 1);

-- +goose Down
DELETE FROM content_items WHERE name = '破魔矢';
