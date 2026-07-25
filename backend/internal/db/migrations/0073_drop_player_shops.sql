-- +goose Up
-- 商店街(個人商店)機能の廃止。持ち物の販売は闇市(yami_items)、店の経営は
-- 家の店(house_shops/house_shop_stock)、さい銭は家のさい銭箱(saisen_log)に
-- それぞれレガシー忠実な実装があり完全に重複するため、専用テーブルを削除する。
DROP TABLE IF EXISTS offering_log;
DROP TABLE IF EXISTS shop_listings;
DROP TABLE IF EXISTS shops;

-- +goose Down
-- 0034_shops.sqlと同じ定義で再作成する(削除済みデータは戻らない)。
CREATE TABLE shops (
    owner_id  BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    name      TEXT NOT NULL,
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE shop_listings (
    owner_id BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    item_id  BIGINT NOT NULL REFERENCES content_items(id) ON DELETE RESTRICT,
    price    BIGINT NOT NULL CHECK (price >= 0),
    stock    INT    NOT NULL DEFAULT 0 CHECK (stock >= 0),
    PRIMARY KEY (owner_id, item_id)
);
CREATE TABLE offering_log (
    id         BIGSERIAL PRIMARY KEY,
    from_id    BIGINT REFERENCES players(id) ON DELETE SET NULL,
    to_id      BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    amount     BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_offering_to ON offering_log (to_id, created_at);
