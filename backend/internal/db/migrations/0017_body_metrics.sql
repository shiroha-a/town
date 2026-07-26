-- +goose Up
-- Phase C: 身長・体重・BMI(体型)。
-- player_status に身長/体重を追加。体重はg単位で保持し小数を回避(表示は/1000でkg)。
-- 既存プレイヤーは平均値(160cm/55kg)でバックフィルし、その後DEFAULTを外して
-- 以後は登録時にサーバRNGが必ず初期値を与える契約とする。
ALTER TABLE player_status
  ADD COLUMN height_cm INT NOT NULL DEFAULT 160,
  ADD COLUMN weight_g  INT NOT NULL DEFAULT 55000;
ALTER TABLE player_status ALTER COLUMN height_cm DROP DEFAULT;
ALTER TABLE player_status ALTER COLUMN weight_g DROP DEFAULT;

-- content_jobs に体型・身長の就労条件列を用意する(値の適用はPhase E)。
-- 0/NULL は条件スキップ。bmi_min/bmi_max は就労時、height_min は求職時に判定。
ALTER TABLE content_jobs
  ADD COLUMN bmi_min    INT,
  ADD COLUMN bmi_max    INT,
  ADD COLUMN height_min INT;

-- +goose Down
ALTER TABLE content_jobs
  DROP COLUMN bmi_min,
  DROP COLUMN bmi_max,
  DROP COLUMN height_min;
ALTER TABLE player_status
  DROP COLUMN height_cm,
  DROP COLUMN weight_g;
