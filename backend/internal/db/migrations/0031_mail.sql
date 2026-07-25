-- +goose Up
-- メール(プレイヤー間メッセージング)。レガシー command.pl mail_do/mail_sousin。
-- 仕様: .tmp/legacy_spec/13_mail_shakai.md パート1。
-- レガシーは送信側・受信側で2レコードを複製保存する。リライトも同様に、箱の持ち主視点の
-- direction列(received/sent)で1テーブルに持つ。本文はプレーンテキストで保存し、表示時に
-- エスケープする(HTMLインジェクション対策)。

CREATE TABLE messages (
    id               BIGSERIAL PRIMARY KEY,
    owner_id         BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE, -- 箱の持ち主
    direction        TEXT   NOT NULL CHECK (direction IN ('received', 'sent')),
    counterpart_id   BIGINT REFERENCES players(id) ON DELETE SET NULL,          -- 相手(削除時null)
    counterpart_name TEXT   NOT NULL,                                           -- 相手名スナップショット
    body             TEXT   NOT NULL,
    saved            BOOLEAN NOT NULL DEFAULT false,                            -- 保存フラグ(FIFO保護)
    sent_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_messages_owner ON messages (owner_id, sent_at DESC);

-- 受信箱を最後に開いた時刻(新着判定用)。
CREATE TABLE mail_check (
    player_id       BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    last_checked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE mail_check;
DROP TABLE messages;
