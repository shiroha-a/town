-- +goose Up
-- 目安箱(要望・不具合の投稿所)。GitHub issue のうち、この規模で効く要素だけを持つ:
-- 種別・状態・コメント・賛同。
--
-- author_name は投稿時の街での名前を焼き付ける。退会しても投稿は残したいので
-- author_id は SET NULL にする(house_bbs と同じ扱い)。
CREATE TABLE feedback_posts (
    id          BIGSERIAL PRIMARY KEY,
    author_id   BIGINT REFERENCES players(id) ON DELETE SET NULL,
    author_name TEXT   NOT NULL,
    -- bug=不具合 / request=要望 / question=質問
    kind        TEXT   NOT NULL CHECK (kind IN ('bug', 'request', 'question')),
    -- open=受付 / triage=検討中 / doing=対応中 / done=対応済み / wontfix=見送り
    status      TEXT   NOT NULL DEFAULT 'open'
                CHECK (status IN ('open', 'triage', 'doing', 'done', 'wontfix')),
    title       TEXT   NOT NULL,
    body        TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_feedback_posts_created ON feedback_posts (created_at DESC);

CREATE TABLE feedback_comments (
    id          BIGSERIAL PRIMARY KEY,
    post_id     BIGINT NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
    author_id   BIGINT REFERENCES players(id) ON DELETE SET NULL,
    author_name TEXT   NOT NULL,
    -- 運営の返信として目立たせる印。投稿時のロールを焼き付けるので、あとで
    -- 管理者を外れても当時の返信は運営の返信のまま残る。
    is_staff    BOOLEAN NOT NULL DEFAULT FALSE,
    body        TEXT   NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_feedback_comments_post ON feedback_comments (post_id, created_at);

-- 賛同(👍)。1人1票を複合主キーで担保する。
CREATE TABLE feedback_votes (
    post_id    BIGINT NOT NULL REFERENCES feedback_posts(id) ON DELETE CASCADE,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, player_id)
);

-- 目安箱を街の施設として置く(公園の空きマス)。default_map.json は新規
-- インストール用で、すでに保存済みの盤面には効かないためここでも足す。
UPDATE town_map
SET facilities = facilities || jsonb_build_array(jsonb_build_object(
      'key', 'meyasu', 'img', 'meyasu', 'alt', '目安箱',
      'town', 0, 'col', 6, 'row', 6, 'dest', 0, 'ready', true))
WHERE id = 1
  AND NOT facilities @> '[{"key": "meyasu"}]'::jsonb
  AND NOT facilities @> '[{"town": 0, "col": 6, "row": 6}]'::jsonb;

-- +goose Down
UPDATE town_map
SET facilities = (
  SELECT COALESCE(jsonb_agg(f ORDER BY ord), '[]'::jsonb)
  FROM jsonb_array_elements(facilities) WITH ORDINALITY AS t(f, ord)
  WHERE NOT (f @> '{"key": "meyasu"}'::jsonb)
)
WHERE id = 1;
DROP TABLE feedback_votes;
DROP TABLE feedback_comments;
DROP TABLE feedback_posts;
