-- +goose Up
-- プロフィールの名前・自己紹介で使われているカスタム絵文字の名前->URL辞書。
-- 投稿本文の絵文字(ライセンス必須)とは別扱い: こちらは相手のインスタンスが
-- 自分のユーザーを描画するために配っているもので、fediverseの通常の表示に当たる。
ALTER TABLE misskey_profiles ADD COLUMN emojis JSONB NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE misskey_profiles DROP COLUMN emojis;
