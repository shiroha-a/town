-- +goose Up
-- Phase F(design 17.9): energy_max / nou_energy_max を固定10からパラメータ由来(旧basic.cgi式)へ。
-- 既存プレイヤーの上限を再計算し、現在値を新上限にクランプする。以後は登録時と
-- パラメータ変化時(applyEffect)にサーバが RefreshPowerMax で再計算する。
UPDATE player_status ps SET
  energy_max = m.emax, nou_energy_max = m.nmax,
  energy = LEAST(ps.energy, m.emax),
  nou_energy = LEAST(ps.nou_energy, m.nmax)
FROM (
  SELECT player_id,
    (FLOOR(looks/12.0 + tairyoku/4.0 + kenkou/4.0 + speed/8.0
           + power/8.0 + wanryoku/8.0 + kyakuryoku/8.0) + 1)::int AS emax,
    (FLOOR(kokugo/6.0 + suugaku/6.0 + rika/6.0 + syakai/6.0
           + eigo/6.0 + ongaku/6.0 + bijutsu/6.0) + 1)::int AS nmax
  FROM player_status
) m
WHERE ps.player_id = m.player_id;

-- +goose Down
-- 固定10へ戻す。
UPDATE player_status SET energy_max = 10, nou_energy_max = 10;
