-- +goose Up
-- 「どこから取り込んだか」しか書かれていないライセンス(例: import from misskey.io)は
-- ライセンス表明ではないため、使用可の判定を厳しくした。既に許可済みのものを取り消す。
-- 消えた絵文字は投稿本文ではショートコードのテキスト表示に戻る(本文は無傷)。
DELETE FROM misskey_emojis
WHERE btrim(regexp_replace(license, '^[ \t]*import(ed)?[ \t]+from[ \t].*$', '', 'gin')) = '';

-- +goose Down
-- 取り消したキャッシュは再度 resolve すれば入り直すため、戻しは不要。
SELECT 1;
