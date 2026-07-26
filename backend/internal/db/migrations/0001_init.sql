-- +goose Up

-- プレイヤー本体。識別キーは (instance_host, remote_user_id)。
-- ユーザー名は複数インスタンス間で一意にならないため主キーにしない。
CREATE TABLE players (
    id             BIGSERIAL PRIMARY KEY,
    instance_host  TEXT NOT NULL,
    remote_user_id TEXT NOT NULL,
    display_name   TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ,
    UNIQUE (instance_host, remote_user_id)
);

-- ロール(admin / moderator / user / restricted)。1プレイヤーが複数保持可能。
CREATE TABLE player_roles (
    player_id BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    role      TEXT   NOT NULL,
    PRIMARY KEY (player_id, role)
);

-- 現在値のステータス行(読み取り高速用)。変更履歴は status_history に残す。
CREATE TABLE player_status (
    player_id      BIGINT PRIMARY KEY REFERENCES players(id) ON DELETE CASCADE,
    energy         INT NOT NULL DEFAULT 10,
    energy_max     INT NOT NULL DEFAULT 10,
    nou_energy     INT NOT NULL DEFAULT 10,
    nou_energy_max INT NOT NULL DEFAULT 10,
    job            TEXT NOT NULL DEFAULT '学生',
    job_level      INT  NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 全ステータス変更の追記型履歴(監査・巻き戻し用)。
CREATE TABLE status_history (
    id         BIGSERIAL PRIMARY KEY,
    player_id  BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    field      TEXT NOT NULL,
    old_value  TEXT,
    new_value  TEXT,
    reason     TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX status_history_player_idx ON status_history(player_id);

-- マネー台帳(複式・追記専用)。1トランザクションのentryの合計は必ず0。
-- 残高は ledger_entry からの導出値。
CREATE TABLE ledger_tx (
    id         BIGSERIAL PRIMARY KEY,
    reason     TEXT NOT NULL,
    ref        TEXT, -- 冪等キー。同一refの二重postはno-op
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX ledger_tx_ref_uniq ON ledger_tx(ref) WHERE ref IS NOT NULL;

CREATE TABLE ledger_entry (
    id         BIGSERIAL PRIMARY KEY,
    tx_id      BIGINT NOT NULL REFERENCES ledger_tx(id) ON DELETE RESTRICT,
    account    TEXT   NOT NULL, -- 'player:<id>' または 'system:<faucet/sink名>'
    delta      BIGINT NOT NULL, -- 符号付き整数(円)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entry_account_idx ON ledger_entry(account);

-- データ駆動コンテンツ。効果/条件は effect スキーマ(JSONB)で表現する。
CREATE TABLE content_items (
    id                    BIGSERIAL PRIMARY KEY,
    name                  TEXT NOT NULL,
    category              TEXT,
    price                 BIGINT NOT NULL DEFAULT 0,
    effect                JSONB NOT NULL DEFAULT '[]',
    effect_schema_version INT NOT NULL DEFAULT 1,
    enabled               BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE content_jobs (
    id                    BIGSERIAL PRIMARY KEY,
    name                  TEXT NOT NULL,
    requirements          JSONB NOT NULL DEFAULT '[]',
    effect                JSONB NOT NULL DEFAULT '[]',
    effect_schema_version INT NOT NULL DEFAULT 1,
    enabled               BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE content_events (
    id                    BIGSERIAL PRIMARY KEY,
    name                  TEXT NOT NULL,
    conditions            JSONB NOT NULL DEFAULT '[]',
    effect                JSONB NOT NULL DEFAULT '[]',
    weight                INT NOT NULL DEFAULT 1,
    effect_schema_version INT NOT NULL DEFAULT 1,
    enabled               BOOLEAN NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 日次ジョブの冪等性保証。ゲーム日ごとに1回だけ実行される。
CREATE TABLE worker_jobs (
    job_date DATE NOT NULL,
    job_type TEXT NOT NULL,
    ran_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (job_date, job_type)
);

-- +goose Down
DROP TABLE worker_jobs;
DROP TABLE content_events;
DROP TABLE content_jobs;
DROP TABLE content_items;
DROP TABLE ledger_entry;
DROP TABLE ledger_tx;
DROP TABLE status_history;
DROP TABLE player_status;
DROP TABLE player_roles;
DROP TABLE players;
