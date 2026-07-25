-- +goose Up
-- 株式会社の会社BBS(レガシーkaishiya_bbs.cgi)。boardは'open'(来訪者)/'member'(役員)。
-- statusは入退会ワークフロー: 'in'=入会希望 'out'=退会希望 'm_ryoukai'=受領済み。
CREATE TABLE company_bbs (
    id BIGSERIAL PRIMARY KEY,
    house_id BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    board TEXT NOT NULL,
    no INT NOT NULL,
    author_id BIGINT,
    author_name TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX company_bbs_house_idx ON company_bbs (house_id, board, id DESC);

-- 株式会社の製造原料(レガシーitemyou.cgi)。syokuは食材購入で蓄積した食料(kcal)。
CREATE TABLE company_materials (
    house_id BIGINT PRIMARY KEY REFERENCES player_houses(id) ON DELETE CASCADE,
    syoku NUMERIC NOT NULL DEFAULT 0,
    last_made_on DATE,
    product_seq INT NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE company_materials;
DROP TABLE company_bbs;
