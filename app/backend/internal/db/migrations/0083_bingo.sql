-- +goose Up
-- ビンゴ大会(レガシー bingo.cgi)。街全体で数日かけて行う共有イベントで、
-- 全プレイヤーが同じ抽選番号を見る。抽選順は開催時に一度だけシャッフルして保存し、
-- 「何個まで公開済みか」は開始からの経過日数で導出する(日次ジョブ不要)。
CREATE TABLE bingo_events (
    id             BIGSERIAL PRIMARY KEY,
    numbers        INT[] NOT NULL,           -- 1..N をシャッフルした抽選順
    per_day        INT NOT NULL,             -- 1日に公開する個数
    days           INT NOT NULL,             -- 開催日数
    lines_to_win   INT NOT NULL,             -- ビンゴ成立に必要なライン数
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_count INT NOT NULL DEFAULT 0,   -- 上がった人数(着順の採番に使う)
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- プレイヤーのカード。numbersは25マス(中央=0でフリー)。
CREATE TABLE bingo_cards (
    id         BIGSERIAL PRIMARY KEY,
    event_id   BIGINT NOT NULL REFERENCES bingo_events(id) ON DELETE CASCADE,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    numbers    INT[] NOT NULL,
    rank       INT,                          -- 上がった着順(NULL=未上がり)
    prize      BIGINT NOT NULL DEFAULT 0,
    claimed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_bingo_cards_player ON bingo_cards (event_id, player_id);

-- +goose Down
DROP TABLE bingo_cards;
DROP TABLE bingo_events;
