-- +goose Up
-- 凍結(ログイン不可)。荒らしへの対応が論理削除しか無く、「一時的に止める」が
-- できなかった。退会とは別物で、解除できる。
--
-- 無期限を別のbool列にせず 'infinity' で表すのは、判定を
-- `suspended_until IS NOT NULL AND now() < suspended_until` の1本に保つため。
-- 列が2つあると「boolはtrueだが日時は過去」のような食い違いが必ず生まれる。
ALTER TABLE players
    ADD COLUMN suspended_until TIMESTAMPTZ,
    ADD COLUMN suspend_reason  TEXT NOT NULL DEFAULT '';

-- 凍結中の住民を引くのは管理画面とログイン判定だけなので、部分索引で足りる。
CREATE INDEX players_suspended_idx ON players (suspended_until)
    WHERE suspended_until IS NOT NULL;

-- +goose Down
DROP INDEX players_suspended_idx;
ALTER TABLE players DROP COLUMN suspended_until, DROP COLUMN suspend_reason;
