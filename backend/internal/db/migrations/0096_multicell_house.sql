-- +goose Up
-- マルチセル(2x1)の家。レガシーは1マス固定で、これはリライトの独自追加
-- (.tmp/design_multicell_house.md)。建築許可証アイテムを消費し、建築費10倍で
-- 横2マスの家を建てられるようにする。

-- 建築許可証。0=通常のアイテム / 2=2マス(2x1)の家を建てられる許可証。
-- 将来2x2を足すときは値を増やす。
ALTER TABLE content_items ADD COLUMN build_span INT NOT NULL DEFAULT 0;

-- 家の横幅(マス数)。既存の家はすべて1マス。
ALTER TABLE player_houses ADD COLUMN span_w INT NOT NULL DEFAULT 1
    CHECK (span_w >= 1);

-- 家が占有するマス。player_houses の UNIQUE (town, grid_row, grid_col) は
-- 原点マスしか守れないため、2マス目との重なりをDB側で防ぐ実体テーブルを持つ。
CREATE TABLE player_house_cells (
    house_id BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    town     INT NOT NULL,
    grid_row INT NOT NULL,
    grid_col INT NOT NULL,
    PRIMARY KEY (town, grid_row, grid_col)
);
CREATE INDEX idx_house_cells_house ON player_house_cells (house_id);

-- 既存の家(すべて1マス)を占有マスへ移す。
INSERT INTO player_house_cells (house_id, town, grid_row, grid_col)
SELECT id, town, grid_row, grid_col FROM player_houses;

-- 建築許可証。デパートに並び、シリアルコードでも配れる通常の持ち物。
-- 価格・有効/無効は管理画面から変えられる。
INSERT INTO content_items (name, category, price, build_span, stock_master)
VALUES ('建築許可証', 'デパート', 100000000, 2, 1);

-- +goose Down
DELETE FROM content_items WHERE name = '建築許可証' AND build_span = 2;
DROP TABLE player_house_cells;
ALTER TABLE player_houses DROP COLUMN span_w;
ALTER TABLE content_items DROP COLUMN build_span;
