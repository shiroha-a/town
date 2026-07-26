-- +goose Up

-- 職業システムの整理。content_jobs.requirements は「その職業に就くための条件」、
-- effect は「働いたときの効果(給料と消費パワー)」とする。
-- 学生(初期職業)は content_jobs に持たず、働けない。職業安定所で転職して初めて
-- 仕事ボタンが出現する(レガシー: town_maker.cgi の if($job ne "学生"))。

-- アルバイトは誰でも就ける(就職条件なし)。働くと1000円、身体パワー-1。
UPDATE content_jobs SET requirements = '[]' WHERE name = 'アルバイト';

-- 転職先の選択肢を増やす。宅配ドライバーは給料が高いが消費も大きい。
INSERT INTO content_jobs (name, requirements, effect) VALUES
('宅配ドライバー',
 '[]',
 '[{"op":"add_money","amount":2500},{"op":"add_param","param":"energy","amount":-3}]');

-- +goose Down
DELETE FROM content_jobs WHERE name = '宅配ドライバー';
UPDATE content_jobs SET requirements = '[{"pred":"param_gte","param":"energy","value":1}]' WHERE name = 'アルバイト';
