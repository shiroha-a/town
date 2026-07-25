-- +goose Up
-- 持ち物販売店(闇市)の売り場(レガシー3_log.cgi)。1行=1品(単品スナップショット)。
-- zokusei=1は倉庫(訪問者に非表示・購入不可)。家の売却時はDoSellHouseが持ち主の
-- 持ち物へ戻すため、CASCADEは保険。
CREATE TABLE yami_items (
    id BIGSERIAL PRIMARY KEY,
    house_id BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    item_id BIGINT NOT NULL REFERENCES content_items(id),
    price BIGINT NOT NULL,
    uses INT NOT NULL DEFAULT 1,
    zokusei INT NOT NULL DEFAULT 0,
    listed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX yami_items_house_idx ON yami_items(house_id);

-- +goose Down
DROP TABLE yami_items;
