-- +goose Up
-- マイホーム(フェーズ3a): 家のマウスオーバーコメントと、訪問時のさい銭ログ。
ALTER TABLE player_houses ADD COLUMN setumei TEXT NOT NULL DEFAULT '';

-- さい銭ログ: 日次上限(同一相手20000円/日、相手受取総額100000円/日)の集計に使う。
CREATE TABLE saisen_log (
    id         BIGSERIAL PRIMARY KEY,
    from_id    BIGINT NOT NULL REFERENCES players(id),
    to_id      BIGINT NOT NULL REFERENCES players(id),
    amount     BIGINT NOT NULL,
    game_date  DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_saisen_log_daily ON saisen_log(to_id, game_date);
CREATE INDEX idx_saisen_log_pair ON saisen_log(from_id, to_id, game_date);

-- +goose Down
DROP TABLE saisen_log;
ALTER TABLE player_houses DROP COLUMN setumei;
