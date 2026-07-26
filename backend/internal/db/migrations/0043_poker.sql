-- +goose Up
-- ポーカー(5カードドロー、得点式)の進行状態。1プレイヤー1ゲーム。
-- points=所持ポイント(5000円=5点で購入)、handは5枚のカード(0..51)。
-- phase=none(未購入)/ready(購入済み・配札前)/dealt(配札済み・交換前)。
CREATE TABLE player_poker (
    player_id  BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    points     INT    NOT NULL DEFAULT 0,
    hand       INT[]  NOT NULL DEFAULT '{}',
    phase      TEXT   NOT NULL DEFAULT 'none',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE player_poker;
