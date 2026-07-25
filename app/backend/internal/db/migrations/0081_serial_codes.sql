-- +goose Up
-- シリアルコード(レガシー tokuten.cgi「特典」の再設計)。
-- レガシーは合言葉を2つだけ持てる固定レコードだったが、リライトでは管理画面から
-- 何本でも発行でき、誰がいつ使ったかを記録する。報酬はアイテムと効果(パラメータ/
-- お金/体重/身長/病気)の両方を設定できる。
-- マルチセルの家の建築許可証の配布にも使う想定(.tmp/design_multicell_house.md)。
CREATE TABLE serial_codes (
    id               BIGSERIAL PRIMARY KEY,
    code             TEXT NOT NULL UNIQUE,   -- 入力するコード(大文字で正規化して保存)
    label            TEXT NOT NULL DEFAULT '', -- 管理用の説明(何のコードか)
    message          TEXT NOT NULL DEFAULT '', -- 受け取り時にプレイヤーへ出す文言
    effect           JSONB NOT NULL DEFAULT '[]'::jsonb, -- 効果エンジンのop配列
    reward_item_id   BIGINT REFERENCES content_items(id) ON DELETE RESTRICT,
    reward_item_uses INT NOT NULL DEFAULT 0,  -- 付与する耐久(0=アイテム無し)
    max_uses         INT NOT NULL DEFAULT 1,  -- 使える回数(0=無制限)
    used_count       INT NOT NULL DEFAULT 0,
    starts_at        TIMESTAMPTZ,             -- 有効期間(NULL=制限なし)
    ends_at          TIMESTAMPTZ,
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 誰がいつ使ったか。同じコードは1人1回まで(UNIQUEで担保)。
CREATE TABLE serial_code_uses (
    code_id     BIGINT NOT NULL REFERENCES serial_codes(id) ON DELETE CASCADE,
    player_id   BIGINT NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    player_name TEXT   NOT NULL,             -- 退会後も記録が読めるよう非正規化
    used_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (code_id, player_id)
);
CREATE INDEX idx_serial_code_uses_code ON serial_code_uses (code_id, used_at DESC);

-- +goose Down
DROP TABLE serial_code_uses;
DROP TABLE serial_codes;
