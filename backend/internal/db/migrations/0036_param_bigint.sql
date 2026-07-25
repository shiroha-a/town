-- +goose Up
-- レガシー準拠でパラメータを青天井にするため、16スキルパラメータをBIGINTへ拡張する。
-- パラメータから導出される身体/頭脳パワー(energy/nou_energy とその上限)も、
-- パラメータが巨大化した際のオーバーフローを避けるためBIGINTへ拡張する。
-- (satiety/weight_g/disease_index/height_cm は独自の小さい範囲なのでINTのまま)
ALTER TABLE player_status
    ALTER COLUMN kokugo         TYPE BIGINT,
    ALTER COLUMN suugaku        TYPE BIGINT,
    ALTER COLUMN rika           TYPE BIGINT,
    ALTER COLUMN syakai         TYPE BIGINT,
    ALTER COLUMN eigo           TYPE BIGINT,
    ALTER COLUMN ongaku         TYPE BIGINT,
    ALTER COLUMN bijutsu        TYPE BIGINT,
    ALTER COLUMN looks          TYPE BIGINT,
    ALTER COLUMN tairyoku       TYPE BIGINT,
    ALTER COLUMN kenkou         TYPE BIGINT,
    ALTER COLUMN speed          TYPE BIGINT,
    ALTER COLUMN power          TYPE BIGINT,
    ALTER COLUMN wanryoku       TYPE BIGINT,
    ALTER COLUMN kyakuryoku     TYPE BIGINT,
    ALTER COLUMN love           TYPE BIGINT,
    ALTER COLUMN omoshirosa     TYPE BIGINT,
    ALTER COLUMN energy         TYPE BIGINT,
    ALTER COLUMN energy_max     TYPE BIGINT,
    ALTER COLUMN nou_energy     TYPE BIGINT,
    ALTER COLUMN nou_energy_max TYPE BIGINT;

-- +goose Down
ALTER TABLE player_status
    ALTER COLUMN kokugo         TYPE INT USING LEAST(kokugo, 2147483647),
    ALTER COLUMN suugaku        TYPE INT USING LEAST(suugaku, 2147483647),
    ALTER COLUMN rika           TYPE INT USING LEAST(rika, 2147483647),
    ALTER COLUMN syakai         TYPE INT USING LEAST(syakai, 2147483647),
    ALTER COLUMN eigo           TYPE INT USING LEAST(eigo, 2147483647),
    ALTER COLUMN ongaku         TYPE INT USING LEAST(ongaku, 2147483647),
    ALTER COLUMN bijutsu        TYPE INT USING LEAST(bijutsu, 2147483647),
    ALTER COLUMN looks          TYPE INT USING LEAST(looks, 2147483647),
    ALTER COLUMN tairyoku       TYPE INT USING LEAST(tairyoku, 2147483647),
    ALTER COLUMN kenkou         TYPE INT USING LEAST(kenkou, 2147483647),
    ALTER COLUMN speed          TYPE INT USING LEAST(speed, 2147483647),
    ALTER COLUMN power          TYPE INT USING LEAST(power, 2147483647),
    ALTER COLUMN wanryoku       TYPE INT USING LEAST(wanryoku, 2147483647),
    ALTER COLUMN kyakuryoku     TYPE INT USING LEAST(kyakuryoku, 2147483647),
    ALTER COLUMN love           TYPE INT USING LEAST(love, 2147483647),
    ALTER COLUMN omoshirosa     TYPE INT USING LEAST(omoshirosa, 2147483647),
    ALTER COLUMN energy         TYPE INT USING LEAST(energy, 2147483647),
    ALTER COLUMN energy_max     TYPE INT USING LEAST(energy_max, 2147483647),
    ALTER COLUMN nou_energy     TYPE INT USING LEAST(nou_energy, 2147483647),
    ALTER COLUMN nou_energy_max TYPE INT USING LEAST(nou_energy_max, 2147483647);
