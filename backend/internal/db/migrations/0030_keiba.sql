-- +goose Up
-- 競馬場(keiba)。レガシー keiba.cgi。6頭立てレースに馬券を賭ける。
-- リライトはサーバ権威: レースをサーバ生成してDB保持し、賭けは馬インデックス+枚数のみ
-- 送信させる(オッズ改竄不能)。仕様: .tmp/legacy_spec/10_kabu_keiba.md パート2。

-- プレイヤーの現在レース(GETで生成/更新、betでrace_id照合)。lineup=[{name,img,odds}]×6。
CREATE TABLE keiba_race (
    player_id  BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    race_id    BIGSERIAL,
    lineup     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ギャンブル王ランキング。儲け = won - invested。90日無活動は表示から除外。
CREATE TABLE keiba_ranking (
    player_id   BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    invested    BIGINT NOT NULL DEFAULT 0,   -- 総投入額
    won         BIGINT NOT NULL DEFAULT 0,   -- 総獲得額
    last_played TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE keiba_ranking;
DROP TABLE keiba_race;
