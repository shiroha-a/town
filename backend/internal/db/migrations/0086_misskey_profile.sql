-- +goose Up
-- Misskeyプロフィールの表示とリモートフォロー。仕様: .tmp/design_prof.md

-- 住民のMisskey側プロフィール。表示のたびに相手インスタンスを叩かないための
-- キャッシュで、相手が落ちていても最後に取れた内容で表示を続けられる。
CREATE TABLE misskey_profiles (
    player_id       BIGINT PRIMARY KEY REFERENCES players (id) ON DELETE CASCADE,
    username        TEXT        NOT NULL,
    host            TEXT,                        -- そのインスタンスから見てローカルならNULL
    name            TEXT        NOT NULL DEFAULT '',
    avatar_url      TEXT        NOT NULL DEFAULT '',
    banner_url      TEXT        NOT NULL DEFAULT '',
    description     TEXT        NOT NULL DEFAULT '',
    followers_count INT         NOT NULL DEFAULT 0,
    following_count INT         NOT NULL DEFAULT 0,
    notes_count     INT         NOT NULL DEFAULT 0,
    is_bot          BOOLEAN     NOT NULL DEFAULT FALSE,
    is_cat          BOOLEAN     NOT NULL DEFAULT FALSE,
    is_locked       BOOLEAN     NOT NULL DEFAULT FALSE,
    fetched_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 別インスタンスの相手を閲覧者のインスタンス上で解決した結果。
-- ap/show は 30回/時 と厳しいので、一度引けたら再解決しない。
CREATE TABLE misskey_resolved_users (
    viewer_host      TEXT        NOT NULL,       -- 閲覧者のinstance_host
    target_player_id BIGINT      NOT NULL REFERENCES players (id) ON DELETE CASCADE,
    resolved_user_id TEXT        NOT NULL,       -- viewer_host 側でのuserId
    resolved_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (viewer_host, target_player_id)
);

-- +goose Down
DROP TABLE misskey_resolved_users;
DROP TABLE misskey_profiles;
