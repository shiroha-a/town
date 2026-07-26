-- +goose Up

-- 施設利用の汎用クールタイム。次に利用できる時刻を (player, facility) ごとに保持。
-- トレーニング等はメニューごとに間隔が異なるため、利用時に next_available_at を
-- now()+間隔 に更新する。
CREATE TABLE player_facility_cooldowns (
    player_id         BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    facility          TEXT   NOT NULL,
    next_available_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (player_id, facility)
);

-- ジム(スポーツクラブ)のトレーニングメニュー。
-- effect = 身体パラメータの上昇 + 身体パワー消費(energy負)。use_interval_min = 間隔。
-- 新プレイヤーの energy_max(10)に合わせ、消費はレガシーより小さめにスケール。
INSERT INTO content_items (name, category, price, facility, use_interval_min, effect) VALUES
('スイミング', 'ジム', 2500, 'gym', 30,
 '[{"op":"add_param","param":"energy","amount":-5},{"op":"add_param","param":"looks","amount":4},{"op":"add_param","param":"tairyoku","amount":3},{"op":"add_param","param":"kenkou","amount":2},{"op":"add_param","param":"speed","amount":2},{"op":"add_param","param":"power","amount":1},{"op":"add_param","param":"wanryoku","amount":2},{"op":"add_param","param":"kyakuryoku","amount":2}]'),
('ウォーキング', 'ジム', 800, 'gym', 15,
 '[{"op":"add_param","param":"energy","amount":-2},{"op":"add_param","param":"tairyoku","amount":2},{"op":"add_param","param":"kenkou","amount":3},{"op":"add_param","param":"speed","amount":1},{"op":"add_param","param":"kyakuryoku","amount":2}]'),
('ストレッチ', 'ジム', 600, 'gym', 15,
 '[{"op":"add_param","param":"energy","amount":-1},{"op":"add_param","param":"tairyoku","amount":1},{"op":"add_param","param":"kenkou","amount":2},{"op":"add_param","param":"kyakuryoku","amount":1}]'),
('テニス', 'ジム', 1600, 'gym', 20,
 '[{"op":"add_param","param":"energy","amount":-4},{"op":"add_param","param":"looks","amount":2},{"op":"add_param","param":"tairyoku","amount":2},{"op":"add_param","param":"kenkou","amount":2},{"op":"add_param","param":"speed","amount":2},{"op":"add_param","param":"power","amount":1},{"op":"add_param","param":"kyakuryoku","amount":2}]');

-- +goose Down
DELETE FROM content_items WHERE facility = 'gym';
DROP TABLE player_facility_cooldowns;
