-- +goose Up
-- 家の店(フェーズ4): 家に紐づく店。卸問屋で仕入れた商品を掛け率/個別価格で並べ、
-- 訪問者に販売する。売上は家主の普通口座へ。既存の簡略shopとは別系統。
CREATE TABLE house_shops (
    house_id  BIGINT PRIMARY KEY REFERENCES player_houses(id) ON DELETE CASCADE,
    title     TEXT NOT NULL DEFAULT '',
    syubetu   TEXT NOT NULL,                          -- 店の種類(スーパー等)
    markup    NUMERIC(4, 2) NOT NULL DEFAULT 2.0,     -- 基本販売掛け率(0.3<率<=3)
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 店在庫: 卸問屋で仕入れた商品。sell_priceがNULLなら掛け率で自動計算。
CREATE TABLE house_shop_stock (
    house_id   BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    item_id    BIGINT NOT NULL REFERENCES content_items(id) ON DELETE RESTRICT,
    buy_price  BIGINT NOT NULL,                       -- 仕入れ値(円)
    sell_price BIGINT,                                -- 個別販売価格(NULL=掛け率で計算)
    stock      INT NOT NULL DEFAULT 0 CHECK (stock >= 0),
    PRIMARY KEY (house_id, item_id)
);

-- +goose Down
DROP TABLE house_shop_stock;
DROP TABLE house_shops;
