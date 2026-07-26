-- +goose Up
-- 株取引場(kabu)。レガシー kabu.cgi + event.pl の株システム。
-- 5銘柄A〜Eの共有株価、プレイヤー毎の保有、価格動向ログ、売買記録。
-- 仕様: .tmp/legacy_spec/10_kabu_keiba.md パート1。

-- 共有株価(全プレイヤー共通)。初期値25000でA〜Eをシード。
CREATE TABLE stock_price (
    symbol     CHAR(1) PRIMARY KEY,
    price      BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO stock_price (symbol, price) VALUES
    ('A', 25000), ('B', 25000), ('C', 25000), ('D', 25000), ('E', 25000);

-- 株価動向ログ(表示は最新30件)。workerの変動ジョブが追記する。
CREATE TABLE stock_event_log (
    id         BIGSERIAL PRIMARY KEY,
    message    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- プレイヤー毎の保有株。銘柄ごと1行。shares=保有数(0-200)。
CREATE TABLE player_stock (
    player_id  BIGINT  NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    symbol     CHAR(1) NOT NULL,
    shares     INT    NOT NULL DEFAULT 0,   -- 保有株数
    cost_total BIGINT NOT NULL DEFAULT 0,   -- 保有分の取得原価合計
    inv_total  BIGINT NOT NULL DEFAULT 0,   -- 累計投資額
    ret_total  BIGINT NOT NULL DEFAULT 0,   -- 累計回収額
    PRIMARY KEY (player_id, symbol)
);

-- 売買記録(表示は最新10件/プレイヤー)。
CREATE TABLE stock_trade_log (
    id         BIGSERIAL PRIMARY KEY,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    message    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_stock_trade_log_player ON stock_trade_log (player_id, id DESC);

-- +goose Down
DROP TABLE stock_trade_log;
DROP TABLE player_stock;
DROP TABLE stock_event_log;
DROP TABLE stock_price;
