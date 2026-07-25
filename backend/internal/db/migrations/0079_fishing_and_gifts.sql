-- +goose Up
-- 釣りゲームの進行中セッション(レガシー member/<id>/my_tsuri.cgi)。1人1つ。
-- カードの配置(当たり/継続の位置)はサーバだけが持ち、クライアントには枚数しか返さない
-- (返すとカンニングできる)。スクラッチ(player_scratch_cards)と同じ方針。
CREATE TABLE player_fishing (
    player_id  BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    rank_a     INT NOT NULL,  -- 餌のランク、または引いた継続カードの番号
    rank_b     INT NOT NULL,  -- 4枚時の第2継続カード番号(0=なし)
    cards      INT NOT NULL,  -- 今出ているカードの枚数(2..4)
    win_card   INT NOT NULL,  -- 当たりの位置(1..cards)
    cont_card1 INT NOT NULL,  -- 継続1の位置(0=なし)
    cont_card2 INT NOT NULL,  -- 継続2の位置(0=なし)
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 贈答用に確保したアイテム(レガシーの種別「ギフト」)。自分では使えず、メールで贈る。
-- 分割変換できるので player_items と違い1行1口(idキー)で持つ。
CREATE TABLE player_gifts (
    id         BIGSERIAL PRIMARY KEY,
    owner_id   BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    item_id    BIGINT NOT NULL REFERENCES content_items(id) ON DELETE RESTRICT,
    uses       INT NOT NULL CHECK (uses > 0),  -- 残り耐久(贈るたびに1減る)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_player_gifts_owner ON player_gifts (owner_id, id);

-- メールに添付した贈り物の表示用(送信・受信の両方の行に記録する)。
ALTER TABLE messages ADD COLUMN gift_item_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE messages DROP COLUMN gift_item_name;
DROP TABLE player_gifts;
DROP TABLE player_fishing;
