-- +goose Up
-- 空き地を施設に統合する。既存の town_plots を akichi 施設として town_map.facilities
-- へ移し、town_plots は廃止する。以後、建築可能マスは key='akichi' の施設で表す。
UPDATE town_map SET facilities = facilities || COALESCE(
	(SELECT jsonb_agg(jsonb_build_object(
		'key', 'akichi', 'img', 'akiti', 'alt', '空き地',
		'town', town, 'col', grid_col, 'row', grid_row, 'dest', 0, 'ready', false))
	 FROM town_plots), '[]'::jsonb)
WHERE id = 1;

DROP TABLE town_plots;

-- +goose Down
-- town_plots を再作成する(データは復元されない)。
CREATE TABLE town_plots (
	town     INT NOT NULL,
	grid_row INT NOT NULL,
	grid_col INT NOT NULL,
	PRIMARY KEY (town, grid_row, grid_col)
);
