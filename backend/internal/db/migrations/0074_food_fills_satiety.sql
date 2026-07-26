-- +goose Up
-- 食料品・ファーストフードは食べると満腹になる(レガシー: 食事でharahe=満腹)。
-- 一括投入時にfills_satietyが高級弁当以外に付いていなかったのを修正する。
UPDATE content_items SET fills_satiety = TRUE
 WHERE category IN ('食料品', 'ファーストフード');

-- +goose Down
UPDATE content_items SET fills_satiety = FALSE
 WHERE category IN ('食料品', 'ファーストフード') AND name <> '高級弁当';
