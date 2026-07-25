-- +goose Up

-- ユーザー行動の追跡ログ兼、更新系操作の冪等性ガード。
-- idempotency_key が指定された操作は同一キーの二重実行をno-opにする。
CREATE TABLE action_log (
    id              BIGSERIAL PRIMARY KEY,
    player_id       BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    action_type     TEXT NOT NULL,
    idempotency_key TEXT,
    detail          JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX action_log_idem_uniq ON action_log(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX action_log_player_idx ON action_log(player_id);

-- 効果スキーマの動作確認用シード。仕事(アルバイト)は身体パワーを1消費して
-- 1000円を得る。将来は管理者メニューから追加/編集する。
INSERT INTO content_jobs (name, requirements, effect) VALUES
('アルバイト',
 '[{"pred":"param_gte","param":"energy","value":1}]',
 '[{"op":"add_money","amount":1000},{"op":"add_param","param":"energy","amount":-1}]');

-- +goose Down
DELETE FROM content_jobs WHERE name = 'アルバイト';
DROP TABLE action_log;
