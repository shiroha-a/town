-- +goose Up
-- 店経営(個人商店)。レガシー original_house.cgi §4。
-- プレイヤーが商店を開き、自分の在庫を価格付きで出品、他プレイヤーが訪問購入すると
-- 売上がオーナーの貯金へ入る。仕様: .tmp/legacy_spec/12_kentiku.md §4。
-- 建築(マップに家)・マイタウン・卸・株式会社は後続増分。

CREATE TABLE shops (
    owner_id  BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    name      TEXT NOT NULL,
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 出品。在庫(stock)はオーナーの player_items から移したもの。
CREATE TABLE shop_listings (
    owner_id BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    item_id  BIGINT NOT NULL REFERENCES content_items(id) ON DELETE RESTRICT,
    price    BIGINT NOT NULL CHECK (price >= 0),
    stock    INT    NOT NULL DEFAULT 0 CHECK (stock >= 0),
    PRIMARY KEY (owner_id, item_id)
);

-- さい銭(投げ銭)の記録。日次上限(同一相手/相手合計)の判定に使う。
CREATE TABLE offering_log (
    id         BIGSERIAL PRIMARY KEY,
    from_id    BIGINT REFERENCES players(id) ON DELETE SET NULL,
    to_id      BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    amount     BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_offering_to ON offering_log (to_id, created_at);

-- +goose Down
DROP TABLE offering_log;
DROP TABLE shop_listings;
DROP TABLE shops;
