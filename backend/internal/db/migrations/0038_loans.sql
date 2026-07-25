-- +goose Up
-- 住宅ローン。1プレイヤー1件のみ(完済するまで再借入不可)。完済(kaisuu=0)で行を削除する。
CREATE TABLE player_loans (
    player_id  BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    nitigaku   BIGINT NOT NULL, -- 1日あたりの返済額(日次に普通口座から引き落とす)
    kaisuu     INT    NOT NULL, -- 残り返済回数
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE player_loans;
