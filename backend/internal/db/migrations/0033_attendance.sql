-- +goose Up
-- 足あと(出席簿)。レガシー ashiato1/2.cgi。街アクセス時に日毎の来訪を記録する。
-- 仕様: .tmp/legacy_spec/13_mail_shakai.md パート3。
-- 注記に従い正規化: 行の存在=その日出席。欠席は登録済みで行なしから導出、未登録期間は空白。
-- day はゲーム日(AM5境界)。
CREATE TABLE attendance (
    player_id BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    day       DATE   NOT NULL,
    PRIMARY KEY (player_id, day)
);

-- +goose Down
DROP TABLE attendance;
