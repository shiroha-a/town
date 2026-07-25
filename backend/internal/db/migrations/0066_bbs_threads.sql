-- +goose Up
-- Threaded bulletin board (legacy normal_bbs): parent posts carry a per-house
-- thread number (NO.x); replies reference their parent's thread number.
-- author_job snapshots the poster's job for the （職業） suffix display.
ALTER TABLE house_bbs
    ADD COLUMN thread_no INT,
    ADD COLUMN parent_no INT,
    ADD COLUMN author_job TEXT NOT NULL DEFAULT '';

-- 既存の通常投稿はそれぞれ独立スレッドとして家ごとに通し番号を振る
UPDATE house_bbs b SET thread_no = t.rn
FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY house_id ORDER BY id) AS rn
      FROM house_bbs WHERE kind = 'normal') t
WHERE b.id = t.id;

-- +goose Down
ALTER TABLE house_bbs
    DROP COLUMN thread_no,
    DROP COLUMN parent_no,
    DROP COLUMN author_job;
