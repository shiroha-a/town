-- +goose Up
-- Phase E: 仕事の経験値・レベル・昇給・ボーナス・マスター職。
-- 給料・パワー消費・昇給/ボーナス係数・ランク・前提マスター職を content_jobs の列で表現する。
-- bmi_min/bmi_max/height_min は Phase C(0017)で追加済み。
ALTER TABLE content_jobs
  ADD COLUMN salary         BIGINT NOT NULL DEFAULT 0, -- 基本給(1回)
  ADD COLUMN pay_interval   INT    NOT NULL DEFAULT 1, -- N回出勤ごとにまとめて支給
  ADD COLUMN bonus_rate     INT    NOT NULL DEFAULT 0, -- レベルアップ時ボーナス(給料に対する%)
  ADD COLUMN raise_rate     INT    NOT NULL DEFAULT 0, -- 昇給係数(給料に対する level*% )
  ADD COLUMN rank           INT    NOT NULL DEFAULT 1, -- ランク(星)
  ADD COLUMN require_master TEXT,                       -- 前提マスター職(未マスターだと就けない)
  ADD COLUMN body_cost      INT    NOT NULL DEFAULT 0, -- 出勤時の身体パワー消費(基礎)
  ADD COLUMN nou_cost       INT    NOT NULL DEFAULT 0; -- 出勤時の頭脳パワー消費(基礎)

ALTER TABLE player_status
  ADD COLUMN job_exp       INT    NOT NULL DEFAULT 0,   -- 現在の職業の総経験値
  ADD COLUMN job_kaisuu    INT    NOT NULL DEFAULT 0,   -- 現在の職業での累計勤務回数
  ADD COLUMN mastered_jobs TEXT[] NOT NULL DEFAULT '{}'; -- マスター済み職業(レベル15到達で追加)

-- 既存職業に給与体系を付与。給料/パワー消費は列へ移行するため effect の add_money/energy は廃止する。
UPDATE content_jobs SET salary = 1000, body_cost = 1, rank = 1, pay_interval = 1,
       bonus_rate = 20, raise_rate = 10, effect = '[]' WHERE name = 'アルバイト';
UPDATE content_jobs SET salary = 2500, body_cost = 3, rank = 2, pay_interval = 1,
       bonus_rate = 20, raise_rate = 10, effect = '[]' WHERE name = '宅配ドライバー';

-- マスター職の前提を示すサンプル職(アルバイトをマスターすると就ける)。3回出勤ごとにまとめ払い。
INSERT INTO content_jobs (name, requirements, effect, salary, pay_interval, bonus_rate, raise_rate, rank, require_master, body_cost, nou_cost)
VALUES ('正社員', '[]', '[]', 5000, 3, 30, 15, 3, 'アルバイト', 2, 1);

-- +goose Down
DELETE FROM content_jobs WHERE name = '正社員';
-- 給料をeffectベースへ戻す(down後もDoWorkが動くように)。
UPDATE content_jobs SET effect = '[{"op": "add_money", "amount": 1000}, {"op": "add_param", "param": "energy", "amount": -1}]'
  WHERE name = 'アルバイト';
UPDATE content_jobs SET effect = '[{"op": "add_money", "amount": 2500}, {"op": "add_param", "param": "energy", "amount": -3}]'
  WHERE name = '宅配ドライバー';
ALTER TABLE player_status
  DROP COLUMN job_exp,
  DROP COLUMN job_kaisuu,
  DROP COLUMN mastered_jobs;
ALTER TABLE content_jobs
  DROP COLUMN salary,
  DROP COLUMN pay_interval,
  DROP COLUMN bonus_rate,
  DROP COLUMN raise_rate,
  DROP COLUMN rank,
  DROP COLUMN require_master,
  DROP COLUMN body_cost,
  DROP COLUMN nou_cost;
