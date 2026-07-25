-- +goose Up
-- 施設プリセット(管理画面のタウンマップ編集): 画像・表示名・遷移先を保存し、
-- 背景レイヤーと同様にドラッグ&ドロップで配置できるようにする。
ALTER TABLE town_map ADD COLUMN facility_presets JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE town_map DROP COLUMN facility_presets;
