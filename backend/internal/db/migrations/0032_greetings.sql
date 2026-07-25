-- +goose Up
-- あいさつ(街の一言掲示板/簡易チャット)。レガシー event.pl sub aisatu。
-- 仕様: .tmp/legacy_spec/13_mail_shakai.md パート2。
-- 本文はプレーンテキストで保存(レガシーのa_nameへのinput埋め込み・font/imgタグ直書きは廃止)。
CREATE TABLE greetings (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT REFERENCES players(id) ON DELETE SET NULL,
    user_name  TEXT NOT NULL,
    category   TEXT NOT NULL,                       -- あいさつ/雑談/.../宣伝/管理人
    body       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT '#333333',     -- #rrggbb
    janken     TEXT,                                -- 勝敗表示(勝ち/負け/あいこ)or null
    posted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_greetings_posted ON greetings (posted_at DESC);

-- +goose Down
DROP TABLE greetings;
