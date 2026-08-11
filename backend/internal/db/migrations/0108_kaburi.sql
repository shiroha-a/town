-- +goose Up
-- カード引きの共有卓。街にひとつだけなので1行に固定する。
-- 伏せ札(hidden_card)は「前の人が引いたカード」で、次に引く人には見せない。
-- last_player に players への外部キーは張らない: 住民データの全消し
-- (deploy/reset_user_data.sql の TRUNCATE ... CASCADE)でこの卓まで巻き込まれる。
-- 直前に引いた人を控えているだけなので、消えた人のIDが残っても実害はない。
CREATE TABLE kaburi_table (
    id          SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    hidden_card SMALLINT   NOT NULL,           -- 前の人が引いたカード
    cards       SMALLINT[] NOT NULL,           -- いま場に並んでいるカード(昇順・伏せ札を含む)
    streak      INT        NOT NULL DEFAULT 0, -- かぶらずに続いた回数(場の連鎖)
    last_player BIGINT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO kaburi_table (id, hidden_card, cards, streak)
VALUES (1, 1, '{1,2,3,4,5,6,7,8,9,10}', 0);

-- +goose Down
DROP TABLE kaburi_table;
