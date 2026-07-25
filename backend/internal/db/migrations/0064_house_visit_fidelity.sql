-- +goose Up
-- 家訪問のレガシー忠実化。
-- house_bbs.title: 家主板(gentei)の記事タイトル(レガシーはタイトル+本文のブログ風)。
-- house_contents.url: コンテンツ種別「独自URL」(dokuzi_url)の埋め込み先URL。
ALTER TABLE house_bbs ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE house_contents ADD COLUMN url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE house_contents DROP COLUMN url;
ALTER TABLE house_bbs DROP COLUMN title;
