-- +goose Up
-- 運営/株式会社の社員(レガシー1_log.cgi/2_log.cgi)。paramsは16能力のJSONB。
CREATE TABLE company_staff (
    id BIGSERIAL PRIMARY KEY,
    house_id BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    idx INT NOT NULL,
    params JSONB NOT NULL DEFAULT '{}'::jsonb,
    edu_log TEXT NOT NULL DEFAULT '',
    last_edu_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (house_id, idx)
);

-- 株式会社の役員(レガシーkaishiya_kanri.cgi、オーナーは含まない)。
CREATE TABLE company_officers (
    house_id BIGINT NOT NULL REFERENCES player_houses(id) ON DELETE CASCADE,
    player_id BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (house_id, player_id)
);

-- +goose Down
DROP TABLE company_officers;
DROP TABLE company_staff;
