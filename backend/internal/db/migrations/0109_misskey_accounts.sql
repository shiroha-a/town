-- +goose Up
-- Misskeyアカウントの複数連携。仕様: .tmp/design_misskey_accounts.md
--
-- これまで住民の識別キーは players(instance_host, remote_user_id) そのものだった。
-- 連携先のインスタンスがサービス終了すると二度とログインできなくなるため、
-- ログインの識別をこのテーブルへ切り出す。

CREATE TABLE player_misskey_accounts (
    host           TEXT        NOT NULL,       -- 正規化済みホスト(小文字)
    remote_user_id TEXT        NOT NULL,       -- そのインスタンス上のuserId
    player_id      BIGINT      NOT NULL REFERENCES players (id) ON DELETE CASCADE,
    username       TEXT        NOT NULL DEFAULT '',
    -- トークンは発行元インスタンスでしか通らないので、アカウントごとに持つ。
    token_enc      BYTEA,
    linked_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- 主キーがこの組なので、1つのMisskeyアカウントが2人の住民に紐付くことは起きない。
    PRIMARY KEY (host, remote_user_id)
);
CREATE INDEX idx_player_misskey_accounts_player ON player_misskey_accounts (player_id);

-- 既存の住民を1件ずつ移す。ゲストは実在のアカウントを持たないので対象外。
-- 代表かどうかの列は持たず、players側の値と一致する行を代表として扱う。
INSERT INTO player_misskey_accounts (host, remote_user_id, player_id, username, token_enc)
SELECT p.instance_host, p.remote_user_id, p.id, COALESCE(mp.username, ''), p.misskey_token_enc
  FROM players p
  LEFT JOIN misskey_profiles mp ON mp.player_id = p.id
 WHERE NOT p.is_guest
   AND p.deleted_at IS NULL
   AND p.instance_host <> ''
   AND p.remote_user_id <> ''
ON CONFLICT (host, remote_user_id) DO NOTHING;

-- 連携の追加中は「誰が追加しようとしているか」を保留行に持つ。コールバックの
-- 経路(ログイン/連携追加)はこの列で決め、クライアントの申告には従わない。
ALTER TABLE auth_sessions ADD COLUMN player_id BIGINT REFERENCES players (id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE auth_sessions DROP COLUMN player_id;
DROP TABLE player_misskey_accounts;
