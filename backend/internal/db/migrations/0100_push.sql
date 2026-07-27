-- +goose Up
-- Web Push(通知)。このゲームは「時間で回復してまた来る」構造なので、
-- メール着信・パワー満タン・仕事の解禁を、閉じていても知らせられるようにする。

-- 送信元を名乗るための鍵(VAPID)。設定ファイルを増やさずに済むよう、
-- 起動時に無ければ自動生成して1行だけ持つ。秘密鍵はセッションと同じ扱い。
CREATE TABLE push_keys (
    id          INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    public_key  TEXT NOT NULL,
    private_key TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 端末ごとの購読先。1人が複数の端末から入れるので player_id は重複する。
-- endpoint がブラウザの発行する宛先で、これが実質のID。
CREATE TABLE push_subscriptions (
    endpoint   TEXT PRIMARY KEY,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- 最後に送れた時刻。宛先が死んでいる(410)と分かったら行ごと消すので、
    -- これは主に様子を見るための記録。
    last_ok_at TIMESTAMPTZ
);
CREATE INDEX idx_push_subs_player ON push_subscriptions (player_id);

-- 何を知らせるかは人ごとの設定。既定は全部オフ(通知は勝手に送らない)。
-- *_sent_at は同じ用件を繰り返し送らないための記録。
--   メール : 最後に知らせた時刻。これより新しい未読があれば送る
--   パワー : 満タンで一度送ったら、満タンでなくなるまで送らない(NULLに戻す)
--   仕事   : 同上
CREATE TABLE player_notify (
    player_id      BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    mail_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    energy_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    work_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    mail_sent_at   TIMESTAMPTZ,
    energy_sent_at TIMESTAMPTZ,
    work_sent_at   TIMESTAMPTZ
);

-- +goose Down
DROP TABLE player_notify;
DROP TABLE push_subscriptions;
DROP TABLE push_keys;
