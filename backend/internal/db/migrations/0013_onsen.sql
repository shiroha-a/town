-- +goose Up

-- 温泉の風呂。入浴で身体/頭脳パワーを回復する(レガシーの回復速度倍率は
-- 回復量に簡略化)。facility='onsen'、use_interval_min=0(クールタイムなし)。
INSERT INTO content_items (name, category, price, facility, use_interval_min, effect) VALUES
('普通風呂', '温泉', 500, 'onsen', 0,
 '[{"op":"add_param","param":"energy","amount":5},{"op":"add_param","param":"nou_energy","amount":5}]'),
('特別風呂', '温泉', 2000, 'onsen', 0,
 '[{"op":"add_param","param":"energy","amount":10},{"op":"add_param","param":"nou_energy","amount":10}]'),
('松風呂', '温泉', 10000, 'onsen', 0,
 '[{"op":"add_param","param":"energy","amount":10},{"op":"add_param","param":"nou_energy","amount":10}]');

-- +goose Down
DELETE FROM content_items WHERE facility = 'onsen';
