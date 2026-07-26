-- +goose Up

-- プレイヤーの所持品。数量は0以上(購入で+1、使用で-1)。
CREATE TABLE player_items (
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    item_id    BIGINT NOT NULL REFERENCES content_items(id) ON DELETE RESTRICT,
    quantity   INT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, item_id)
);

-- 効果スキーマの動作確認用シード。effectは「使用時」の効果。
INSERT INTO content_items (name, category, price, effect) VALUES
('栄養ドリンク', 'ドリンク', 500, '[{"op":"add_param","param":"energy","amount":3}]'),
('参考書',       '書籍',     800, '[{"op":"add_param","param":"nou_energy","amount":3}]');

-- +goose Down
DELETE FROM content_items WHERE name IN ('栄養ドリンク', '参考書');
DROP TABLE player_items;
