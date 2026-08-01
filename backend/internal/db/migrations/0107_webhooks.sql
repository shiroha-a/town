-- +goose Up
-- Webhook通知。管理者が見に行かないと気付けないこと(台帳の破れ・日次処理の
-- 失敗・荒らし・新しい住民)を外へ押し出す。
--
-- 宛先ごとに「送るイベント」を持つ。空配列は「すべて」の意味にする。列挙を
-- 別表に正規化しないのは、数が十数個で固定であり、宛先ごとに丸ごと差し替える
-- 使い方しかしないため。
CREATE TABLE webhooks (
    id           BIGSERIAL PRIMARY KEY,
    url          TEXT        NOT NULL,
    label        TEXT        NOT NULL DEFAULT '',
    enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    events       JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- 直近の結果。設定画面で「届いているか」を見せるためだけの記録。
    last_sent_at TIMESTAMPTZ,
    last_status  INT,
    last_error   TEXT        NOT NULL DEFAULT ''
);

-- 送信待ちの控え。イベントが起きた場所では外部へ通信せず、ここへ積むだけに
-- する(行動のトランザクションの中から外へ出すと、失敗したときに巻き戻す/
-- 巻き戻さないの判断が要る)。実際に送るのは worker。
--
-- 宛先ごとに1行にしているのは、成否とリトライ回数が宛先ごとに違うため。
CREATE TABLE webhook_outbox (
    id           BIGSERIAL   PRIMARY KEY,
    webhook_id   BIGINT      NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event        TEXT        NOT NULL,
    severity     TEXT        NOT NULL DEFAULT 'info',
    title        TEXT        NOT NULL,
    body         TEXT        NOT NULL DEFAULT '',
    fields       JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts     INT         NOT NULL DEFAULT 0,
    next_try_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at TIMESTAMPTZ,
    last_error   TEXT        NOT NULL DEFAULT ''
);

-- worker が毎tickで引くのは「まだ送れていない・時刻が来た」ものだけ。
-- 配送済みの行が積み上がっても空振りが安いよう部分索引にする。
CREATE INDEX webhook_outbox_pending_idx
    ON webhook_outbox (next_try_at)
    WHERE delivered_at IS NULL;

-- +goose Down
DROP TABLE webhook_outbox;
DROP TABLE webhooks;
