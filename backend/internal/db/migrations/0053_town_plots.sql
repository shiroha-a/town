-- +goose Up
-- 空き地(town_plots): 管理者が街ごとに「家を建てられるマス」を指定する。
-- 建設会社では、ここに登録されたマスのうち未使用の場所にのみ家を建てられる。
CREATE TABLE town_plots (
    town     INT NOT NULL, -- 街番号(0=公園..4=謎の街)
    grid_row INT NOT NULL, -- グリッド行(0..Rows-1)
    grid_col INT NOT NULL, -- グリッド列(1..Cols)
    PRIMARY KEY (town, grid_row, grid_col)
);

-- +goose Down
DROP TABLE town_plots;
