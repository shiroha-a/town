-- +goose Up

-- 食べ物かどうか。食べ物(食堂メニュー/食料品等)を飲食すると一律で満腹(satiety=100)
-- になる。満腹値は回復量で評価せず、満腹/未満で食事可否を判定する。
ALTER TABLE content_items ADD COLUMN fills_satiety BOOLEAN NOT NULL DEFAULT false;

UPDATE content_items
SET fills_satiety = true
WHERE facility = 'syokudou'
   OR category IN ('食料品', 'デザート', 'ファーストフード', '弁当');

-- +goose Down
ALTER TABLE content_items DROP COLUMN fills_satiety;
