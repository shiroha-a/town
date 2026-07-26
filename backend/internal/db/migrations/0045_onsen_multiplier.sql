-- +goose Up
-- 温泉の継続回復: 入浴中は自然回復を倍率ぶん加速し続ける。入浴時に倍率を記録し、
-- workerの回復計算で実効回復秒を1/倍率にする。両パワーが満タンになるか退室で1(通常)に戻す。
ALTER TABLE player_status
  ADD COLUMN onsen_multiplier INT NOT NULL DEFAULT 1; -- 入浴中の回復倍率(1=入浴していない)

-- +goose Down
ALTER TABLE player_status DROP COLUMN onsen_multiplier;
