-- +goose Up
-- 街のニュース/住民のイベント履歴。レガシー log_dir/mati_news.cgi(街ニュース100件)と
-- log_dir/event_kanri.cgi(イベント記録150件)を1つの追記型テーブルに統合したもの。
-- どちらも「日時・対象者・本文」の同型データなので分けず、街全体に出すかどうかだけ
-- town_wide で切り替える。仕様: .tmp/design_yakuba.md
CREATE TABLE town_news (
    id         BIGSERIAL PRIMARY KEY,
    kind       TEXT NOT NULL,   -- 入居/就職/家/イベント/当選
    actor_id   BIGINT REFERENCES players(id) ON DELETE SET NULL,
    actor_name TEXT NOT NULL,   -- 退会後も記事が読めるよう名前を非正規化して持つ
    message    TEXT NOT NULL,
    good       BOOLEAN,         -- イベントの良悪。NULL=中立(入居/家など)
    town_wide  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX town_news_wide_idx  ON town_news (created_at DESC, id DESC) WHERE town_wide;
CREATE INDEX town_news_actor_idx ON town_news (actor_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE town_news;
