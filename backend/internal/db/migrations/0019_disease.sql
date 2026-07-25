-- +goose Up
-- Phase D: 病気・病院。
-- disease_index は病気指数(旧 byouki_sisuu、基準50)。0未満で発病し、病名は指数から算出する。
-- disease_evaled_at はコンディション評価(病気指数の増減)の最終時刻で、workerのcatch-up評価に使う。
ALTER TABLE player_status
  ADD COLUMN disease_index     INT NOT NULL DEFAULT 50,
  ADD COLUMN disease_evaled_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE player_status
  DROP COLUMN disease_index,
  DROP COLUMN disease_evaled_at;
