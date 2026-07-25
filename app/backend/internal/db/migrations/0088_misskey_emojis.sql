-- +goose Up
-- Misskeyのカスタム絵文字。仕様: .tmp/design_emoji.md
-- 投稿本文には :name@host: だけを保存し、URLも生HTMLも埋めない。

-- 使用が許可された絵文字のキャッシュ。:name@host: と1対1。
-- 一度許可されたものは以後外部へ問い合わせない(表示のたびに相手を叩かない)。
CREATE TABLE misskey_emojis (
    host       TEXT        NOT NULL,   -- 取得元インスタンス
    name       TEXT        NOT NULL,   -- ショートコード
    url        TEXT        NOT NULL,
    license    TEXT        NOT NULL,   -- 未設定のものは弾くのでNOT NULL
    category   TEXT        NOT NULL DEFAULT '',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (host, name)
);

-- 条件を満たさなかった絵文字の短期ネガティブキャッシュ。連打で相手を叩き続けない。
CREATE TABLE misskey_emoji_rejects (
    host       TEXT        NOT NULL,
    name       TEXT        NOT NULL,
    reason     TEXT        NOT NULL CHECK (reason IN ('no_license', 'sensitive', 'local_only', 'not_found')),
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (host, name)
);

-- +goose Down
DROP TABLE misskey_emoji_rejects;
DROP TABLE misskey_emojis;
