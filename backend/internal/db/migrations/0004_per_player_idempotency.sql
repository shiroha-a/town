-- +goose Up

-- 冪等キーはプレイヤー単位でスコープする。グローバル一意だと、別プレイヤーが
-- 偶然同じキーを使ったときに一方の操作が誤ってno-opになってしまう。
DROP INDEX action_log_idem_uniq;
CREATE UNIQUE INDEX action_log_idem_uniq
    ON action_log(player_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX action_log_idem_uniq;
CREATE UNIQUE INDEX action_log_idem_uniq
    ON action_log(idempotency_key) WHERE idempotency_key IS NOT NULL;
