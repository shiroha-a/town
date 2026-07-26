-- +goose Up
-- migration 0016 は食堂メニュー/高級弁当のカロリー・在庫を id ハードコードで設定していた。
-- しかし DB によって高級弁当の有無など id 配置が異なると、1つずつずれた行を更新して
-- しまう(例: フレッシュ環境ではカレーの calorie_g が 900 になる)。ここでは名前+facility で
-- 冪等に設定し直し、id 非依存にする(Phase C の体重増が calorie_g に依存するため)。
-- 既に正しい環境(本番)では同じ値を書くだけの no-op となる。
UPDATE content_items SET calorie_g = 800,  stock_master = 6  WHERE name = 'スッポン'   AND facility = 'syokudou';
UPDATE content_items SET calorie_g = 900,  stock_master = 12 WHERE name = '中華丼'     AND facility = 'syokudou';
UPDATE content_items SET calorie_g = 1000, stock_master = 18 WHERE name = 'カレー'     AND facility = 'syokudou';
UPDATE content_items SET calorie_g = 700,  stock_master = 20 WHERE name = 'オムライス' AND facility = 'syokudou';
UPDATE content_items SET calorie_g = 500,  stock_master = 10 WHERE name = '高級弁当'   AND facility = '';

-- +goose Down
-- 前方修正のみ(値の巻き戻しは行わない)。
SELECT 1;
