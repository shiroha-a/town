-- +goose Up
-- Misskeyの情報(アイコン・自己紹介・フォロワー数など)を街のプロフィールに
-- 載せるかどうか。既定はオフ: 本人が許可するまで他の住民には見せない。
ALTER TABLE players ADD COLUMN profile_public BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE players DROP COLUMN profile_public;
