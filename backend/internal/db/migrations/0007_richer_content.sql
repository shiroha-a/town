-- +goose Up

-- シードのアイテム/職業に詳細パラメータの効果・必要条件を持たせ、
-- デパート/持ち物の「上昇パラメータ」、職業安定所の「必要パラメータ」を
-- 表示できるようにする(レガシー準拠)。

-- 栄養ドリンク: 身体パワー回復 + 体力上昇。
UPDATE content_items
SET effect = '[{"op":"add_param","param":"energy","amount":3},{"op":"add_param","param":"tairyoku","amount":2}]'
WHERE name = '栄養ドリンク';

-- 参考書: 頭脳パワー回復 + 国語上昇。
UPDATE content_items
SET effect = '[{"op":"add_param","param":"nou_energy","amount":3},{"op":"add_param","param":"kokugo","amount":2}]'
WHERE name = '参考書';

-- 宅配ドライバーは体力8以上でないと就けない(必要パラメータの例)。
UPDATE content_jobs
SET requirements = '[{"pred":"param_gte","param":"tairyoku","value":8}]'
WHERE name = '宅配ドライバー';

-- +goose Down
UPDATE content_items SET effect = '[{"op":"add_param","param":"energy","amount":3}]' WHERE name = '栄養ドリンク';
UPDATE content_items SET effect = '[{"op":"add_param","param":"nou_energy","amount":3}]' WHERE name = '参考書';
UPDATE content_jobs SET requirements = '[]' WHERE name = '宅配ドライバー';
