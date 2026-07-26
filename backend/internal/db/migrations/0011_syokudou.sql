-- +goose Up

-- content_items を施設メニューにも流用する。facility='' はデパートの一般商品、
-- 'syokudou' はセントラル食堂のメニュー。
ALTER TABLE content_items ADD COLUMN facility TEXT NOT NULL DEFAULT '';

-- 空腹値(満腹度 0-100)。時間経過で減少し、食事で回復する。
-- 食事のクールタイム(前回食事からの経過)も持つ。
ALTER TABLE player_status
    ADD COLUMN satiety     INT NOT NULL DEFAULT 100,
    ADD COLUMN satiety_updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN last_ate_at TIMESTAMPTZ;

-- 食堂メニューのシード(effect=食べたときの上昇パラメータ + 満腹度回復)。
INSERT INTO content_items (name, category, price, facility, effect) VALUES
('スッポン', '食堂', 9000, 'syokudou',
 '[{"op":"add_param","param":"satiety","amount":50},{"op":"add_param","param":"looks","amount":3},{"op":"add_param","param":"tairyoku","amount":3},{"op":"add_param","param":"kenkou","amount":3},{"op":"add_param","param":"speed","amount":2},{"op":"add_param","param":"power","amount":2},{"op":"add_param","param":"wanryoku","amount":2}]'),
('中華丼', '食堂', 1050, 'syokudou',
 '[{"op":"add_param","param":"satiety","amount":40},{"op":"add_param","param":"tairyoku","amount":2},{"op":"add_param","param":"kenkou","amount":2},{"op":"add_param","param":"speed","amount":1},{"op":"add_param","param":"power","amount":1},{"op":"add_param","param":"wanryoku","amount":1},{"op":"add_param","param":"kyakuryoku","amount":1}]'),
('カレー', '食堂', 750, 'syokudou',
 '[{"op":"add_param","param":"satiety","amount":40},{"op":"add_param","param":"tairyoku","amount":2},{"op":"add_param","param":"kenkou","amount":2},{"op":"add_param","param":"speed","amount":1},{"op":"add_param","param":"wanryoku","amount":1},{"op":"add_param","param":"kyakuryoku","amount":1}]'),
('オムライス', '食堂', 600, 'syokudou',
 '[{"op":"add_param","param":"satiety","amount":35},{"op":"add_param","param":"tairyoku","amount":2},{"op":"add_param","param":"kenkou","amount":1},{"op":"add_param","param":"power","amount":1}]');

-- +goose Down
DELETE FROM content_items WHERE facility = 'syokudou';
ALTER TABLE player_status
    DROP COLUMN satiety,
    DROP COLUMN satiety_updated_at,
    DROP COLUMN last_ate_at;
ALTER TABLE content_items DROP COLUMN facility;
