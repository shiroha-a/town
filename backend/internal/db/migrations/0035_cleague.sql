-- +goose Up
-- Cリーグ(バトルキャラ育成)。レガシー game.cgi sub doukyo/c_league/battle。
-- 1プレイヤー1キャラ。自分の16パラメータとお金を注入して育成し、キャラ同士で対戦する。
-- 仕様: .tmp/legacy_spec/14_school_prof.md §7。恋愛/結婚(kekkon.cgi)とは無関係。
CREATE TABLE battle_characters (
    owner_id      BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    name          TEXT   NOT NULL,
    abilities     JSONB  NOT NULL DEFAULT '{}',  -- 16パラメータ(kokugo..omoshirosa)のmap
    wins          INT    NOT NULL DEFAULT 0,
    losses        INT    NOT NULL DEFAULT 0,
    draws         INT    NOT NULL DEFAULT 0,
    last_match_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE battle_characters;
