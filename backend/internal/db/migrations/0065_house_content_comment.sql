-- +goose Up
-- コンテンツ枠のタイトル下コメント(リード文)。レガシーの bbs1_come / gentei_come /
-- dokuzi_come 相当。訪問画面でコンテンツ見出しの下に表示される。
ALTER TABLE house_contents ADD COLUMN comment TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE house_contents DROP COLUMN comment;
