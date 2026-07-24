-- +goose Up
-- 管理画面で追加/編集/削除できるカスタムイベント。content_eventsは初期設計の
-- placeholder(未使用)だったため、Outcome相当の列を足して実用化する。
-- 発生時の効果: 金額(money_min..money_maxの一様乱数)、パラメータ増減(params)、
-- 病気指数の直接代入(disease_set)、体重増減(weight_g)。weightは抽選の重み。
ALTER TABLE content_events
    ADD COLUMN message TEXT NOT NULL DEFAULT '',
    ADD COLUMN good BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN money_min BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN money_max BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN params JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN disease_set INT,
    ADD COLUMN weight_g INT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE content_events
    DROP COLUMN message,
    DROP COLUMN good,
    DROP COLUMN money_min,
    DROP COLUMN money_max,
    DROP COLUMN params,
    DROP COLUMN disease_set,
    DROP COLUMN weight_g;
