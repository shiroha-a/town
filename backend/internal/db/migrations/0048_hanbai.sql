-- +goose Up
-- 自販機(hanbai): デパートの「ドリンク」カテゴリを自販機用にコピーし、毎日ランダム3品を陳列する。
-- 飲み物系の自動販売機として、家システムに非依存で完結する。
INSERT INTO content_items
  (name, category, price, effect, use_interval_min, facility, fills_satiety,
   durability, durability_unit, stock_master, calorie_g, max_sets, power_multiplier)
SELECT name, category, price, effect, use_interval_min, 'hanbai', fills_satiety,
       durability, durability_unit, stock_master, calorie_g, max_sets, power_multiplier
FROM content_items
WHERE facility = '' AND category = 'ドリンク' AND enabled;

-- +goose Down
DELETE FROM content_items WHERE facility = 'hanbai';
