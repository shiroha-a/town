-- +goose Up
-- 学校: 頭脳科目(国語/数学/理科/社会/英語/音楽/美術)を1日1回だけ大きく伸ばす施設。
-- レガシー command.pl sub school/do_school 準拠(.tmp/legacy_spec/14_school_prof.md §1)。
-- content_items(facility='school')として登録。effect=頭脳パラメータ上昇+頭脳パワー消費(nou_energy負)。
-- price=受講料(円)。1日1回制限は action.DoSchool が player_facility_cooldowns に
-- 次のゲーム日境界をセットして表現する(use_interval_min は未使用)。
-- 頭脳消費はレガシー(14-20)を新プレイヤーの nou_energy_max(概ね6-23)に合わせ約半分にスケール。
-- 金額・パラメータ上昇はレガシー忠実。
INSERT INTO content_items (name, category, price, facility, use_interval_min, effect) VALUES
('英会話講座', '学校', 16000, 'school', 0,
 '[{"op":"add_param","param":"kokugo","amount":1},{"op":"add_param","param":"eigo","amount":10},{"op":"add_param","param":"nou_energy","amount":-8}]'),
('日本語講座', '学校', 14000, 'school', 0,
 '[{"op":"add_param","param":"kokugo","amount":10},{"op":"add_param","param":"nou_energy","amount":-7}]'),
('パソコン教室', '学校', 20000, 'school', 0,
 '[{"op":"add_param","param":"suugaku","amount":8},{"op":"add_param","param":"rika","amount":8},{"op":"add_param","param":"eigo","amount":2},{"op":"add_param","param":"ongaku","amount":3},{"op":"add_param","param":"bijutsu","amount":5},{"op":"add_param","param":"nou_energy","amount":-10}]'),
('デザイン講座', '学校', 15000, 'school', 0,
 '[{"op":"add_param","param":"eigo","amount":2},{"op":"add_param","param":"bijutsu","amount":10},{"op":"add_param","param":"nou_energy","amount":-8}]'),
('ヴォーカル講座', '学校', 18000, 'school', 0,
 '[{"op":"add_param","param":"ongaku","amount":14},{"op":"add_param","param":"nou_energy","amount":-9}]'),
('社会科学講義', '学校', 18000, 'school', 0,
 '[{"op":"add_param","param":"kokugo","amount":3},{"op":"add_param","param":"suugaku","amount":2},{"op":"add_param","param":"rika","amount":2},{"op":"add_param","param":"syakai","amount":8},{"op":"add_param","param":"nou_energy","amount":-9}]');

-- +goose Down
DELETE FROM content_items WHERE facility = 'school';
