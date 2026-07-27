-- +goose Up
-- 店頭に並べるかどうかをアイテムごとに切り替えられるようにする。
--
-- enabled(=無効化)との違い: enabled=false はアイテムそのものを止める設定で、
-- 使用も含めて扱いを消す方向の意味を持つ。shop_listed=false は「店では売らないが
-- 存在はする」もので、シリアルコードやイベントで配って持ち物として使える。
-- 建築許可証のように配布でしか手に入らない品を表すために要る。
--
-- デパート(facility='')だけでなく、自販機・食堂などその品を扱う施設の店頭と、
-- 卸問屋(プレイヤーの店の仕入れ)にも効く。
ALTER TABLE content_items ADD COLUMN shop_listed BOOLEAN NOT NULL DEFAULT TRUE;

-- 建築許可証は販売しない(シリアルコード・イベントでの配布のみ)。
UPDATE content_items SET shop_listed = FALSE WHERE build_span > 0;

-- +goose Down
ALTER TABLE content_items DROP COLUMN shop_listed;
