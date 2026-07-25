-- +goose Up

-- プレイヤーの詳細パラメータ(メイン画面右側の一覧)。
-- 頭脳(教科)・身体・その他。アダルト系(エッチ)はリライトで排除済み。
-- 初期値は控えめな固定値(将来 学校/トレーニングで上昇させる)。
ALTER TABLE player_status
    ADD COLUMN kokugo     INT NOT NULL DEFAULT 5,   -- 国語
    ADD COLUMN suugaku    INT NOT NULL DEFAULT 5,   -- 数学
    ADD COLUMN rika       INT NOT NULL DEFAULT 5,   -- 理科
    ADD COLUMN syakai     INT NOT NULL DEFAULT 5,   -- 社会
    ADD COLUMN eigo       INT NOT NULL DEFAULT 5,   -- 英語
    ADD COLUMN ongaku     INT NOT NULL DEFAULT 5,   -- 音楽
    ADD COLUMN bijutsu    INT NOT NULL DEFAULT 5,   -- 美術
    ADD COLUMN looks      INT NOT NULL DEFAULT 5,   -- ルックス
    ADD COLUMN tairyoku   INT NOT NULL DEFAULT 5,   -- 体力
    ADD COLUMN kenkou     INT NOT NULL DEFAULT 5,   -- 健康
    ADD COLUMN speed      INT NOT NULL DEFAULT 5,   -- スピード
    ADD COLUMN power      INT NOT NULL DEFAULT 5,   -- パワー
    ADD COLUMN wanryoku   INT NOT NULL DEFAULT 5,   -- 腕力
    ADD COLUMN kyakuryoku INT NOT NULL DEFAULT 5,   -- 脚力
    ADD COLUMN love       INT NOT NULL DEFAULT 0,   -- LOVE
    ADD COLUMN omoshirosa INT NOT NULL DEFAULT 5;   -- 面白さ

-- +goose Down
ALTER TABLE player_status
    DROP COLUMN kokugo, DROP COLUMN suugaku, DROP COLUMN rika, DROP COLUMN syakai,
    DROP COLUMN eigo, DROP COLUMN ongaku, DROP COLUMN bijutsu, DROP COLUMN looks,
    DROP COLUMN tairyoku, DROP COLUMN kenkou, DROP COLUMN speed, DROP COLUMN power,
    DROP COLUMN wanryoku, DROP COLUMN kyakuryoku, DROP COLUMN love, DROP COLUMN omoshirosa;
