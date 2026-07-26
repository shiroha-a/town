-- +goose Up
-- お試しプレイ(ゲスト)。Misskeyアカウントを持たない人でも触れるようにする。
-- 住民としては数えず、一定時間で消える。制限はサーバー側(認可)で掛ける。
ALTER TABLE players ADD COLUMN is_guest BOOLEAN NOT NULL DEFAULT FALSE;

-- 掃除ジョブが期限切れのゲストだけを引くための索引。
CREATE INDEX idx_players_guest ON players (created_at) WHERE is_guest;

-- +goose Down
DROP INDEX idx_players_guest;
ALTER TABLE players DROP COLUMN is_guest;
