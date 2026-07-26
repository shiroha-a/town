-- +goose Up
-- 温泉をレガシー準拠に作り直す。旧TOWNの風呂は「自然回復(一定秒で1pt)を N 倍に加速する」
-- 時間ベース方式で、固定量回復ではない。content_items に回復倍率 power_multiplier を追加し、
-- 5種の風呂(普通/特別/梅/竹/松)を用意する。倍率が大きいほど短い放置でMAXまで回復できる。
ALTER TABLE content_items
  ADD COLUMN power_multiplier INT NOT NULL DEFAULT 0; -- 温泉の回復速度倍率(0=温泉ではない)

DELETE FROM content_items WHERE facility = 'onsen';

-- price=料金、power_multiplier=回復倍率。effectは使わない(回復は時間ベースで算出)。
-- 特別風呂は旧仕様では倍率がステータス依存(max(energy_max,nou_energy_max)/50、最低2)だが、
-- 通常のプレイヤーでは最小の2倍/200円になるため静的な最小値で近似する。
INSERT INTO content_items (name, category, price, facility, use_interval_min, effect, power_multiplier) VALUES
('普通風呂', '温泉', 500,   'onsen', 0, '[]', 10),
('特別風呂', '温泉', 200,   'onsen', 0, '[]', 2),
('梅風呂',   '温泉', 10000, 'onsen', 0, '[]', 200),
('竹風呂',   '温泉', 20000, 'onsen', 0, '[]', 400),
('松風呂',   '温泉', 50000, 'onsen', 0, '[]', 1000);

-- +goose Down
DELETE FROM content_items WHERE facility = 'onsen';
INSERT INTO content_items (name, category, price, facility, use_interval_min, effect) VALUES
('普通風呂', '温泉', 500, 'onsen', 0,
 '[{"op":"add_param","param":"energy","amount":5},{"op":"add_param","param":"nou_energy","amount":5}]'),
('特別風呂', '温泉', 2000, 'onsen', 0,
 '[{"op":"add_param","param":"energy","amount":10},{"op":"add_param","param":"nou_energy","amount":10}]'),
('松風呂', '温泉', 10000, 'onsen', 0,
 '[{"op":"add_param","param":"energy","amount":10},{"op":"add_param","param":"nou_energy","amount":10}]');
ALTER TABLE content_items DROP COLUMN power_multiplier;
