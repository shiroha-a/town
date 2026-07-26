-- +goose Up
-- プレイヤーが現在いる街(0=公園..4=謎の街)。街移動で変化する。既存は0(メイン街)。
ALTER TABLE players ADD COLUMN current_town INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE players DROP COLUMN current_town;
