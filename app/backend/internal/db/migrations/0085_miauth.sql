-- +goose Up
-- MiAuth(Misskey認証)とセッション管理。仕様: .tmp/design_miauth.md

-- 参加を許可/拒否するインスタンス。方針(blacklist/whitelist)は settings に持ち、
-- 2つのリストは別々に保持する(モードを切り替えても互いを潰さない)。
CREATE TABLE instance_rules (
    host       TEXT PRIMARY KEY,          -- 正規化済みホスト(小文字)
    kind       TEXT NOT NULL CHECK (kind IN ('block', 'allow')),
    note       TEXT NOT NULL DEFAULT '',  -- 管理用メモ(なぜ登録したか)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- MiAuthのやりとり中の保留セッション(短命)。コールバックで戻ってきた session を
-- ここと突き合わせることで、任意のhostへ問い合わせさせられるのを防ぐ。
CREATE TABLE auth_sessions (
    id         UUID PRIMARY KEY,
    host       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_auth_sessions_created ON auth_sessions (created_at);

-- ログイン済みセッション。cookieにはランダムトークンを入れ、DBにはその
-- SHA-256だけを保存する(DBが漏れてもcookieを再現できない)。
CREATE TABLE sessions (
    token_hash   BYTEA PRIMARY KEY,
    player_id    BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_player ON sessions (player_id);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);

-- Misskeyのアクセストークン(prof表示やリモートフォローで対向APIに提示する)。
-- 提示が必要なので可逆で持つ。運用鍵で暗号化して保存する。
ALTER TABLE players ADD COLUMN misskey_token_enc BYTEA;

-- +goose Down
ALTER TABLE players DROP COLUMN misskey_token_enc;
DROP TABLE sessions;
DROP TABLE auth_sessions;
DROP TABLE instance_rules;
