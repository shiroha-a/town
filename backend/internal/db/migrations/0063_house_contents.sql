-- +goose Up
-- 家のコンテンツ枠(レガシー oriie_settei.cgi の my_con1..4)。内装ランクで枠数が
-- 決まり(A=4..D=1)、各枠に公開するコンテンツ(通常掲示板/お店/家主板)を設定する。
-- 訪問者には設定された枠のコンテンツだけが表示される。
CREATE TABLE house_contents (
    house_id BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    slot     INT NOT NULL,             -- 枠番号(0始まり。ランクの枠数未満)
    kind     TEXT NOT NULL,            -- 'bbs'=通常掲示板 / 'shop'=お店 / 'nushi'=家主板
    title    TEXT NOT NULL DEFAULT '', -- 枠のタイトル(空なら種別の既定名)
    PRIMARY KEY (house_id, slot)
);

-- 既存の家は「掲示板・家主板・(店があれば)店」が常設表示だったため、枠数の許す
-- 範囲でシードして現状の見え方を保つ(優先: 掲示板 > 家主板 > 店)。枠数=4-内装ランク。
INSERT INTO house_contents (house_id, slot, kind)
SELECT id, 0, 'bbs' FROM player_houses;
INSERT INTO house_contents (house_id, slot, kind)
SELECT id, 1, 'nushi' FROM player_houses WHERE 4 - interior_rank >= 2;
INSERT INTO house_contents (house_id, slot, kind)
SELECT h.id, 2, 'shop' FROM player_houses h
JOIN house_shops hs ON hs.house_id = h.id
WHERE 4 - h.interior_rank >= 3;

-- +goose Down
DROP TABLE house_contents;
