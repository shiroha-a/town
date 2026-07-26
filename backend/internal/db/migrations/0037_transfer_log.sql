-- +goose Up
-- 銀行振込の1日上限判定用ログ。同一相手への当日送金合計(相手に届いた額)を集計する。
CREATE TABLE transfer_log (
    id         BIGSERIAL PRIMARY KEY,
    from_id    BIGINT NOT NULL,
    to_id      BIGINT NOT NULL,
    amount     BIGINT NOT NULL, -- 相手に届いた額(寄付として消える超過分は含まない)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX transfer_log_from_to_idx ON transfer_log(from_id, to_id, created_at);

-- +goose Down
DROP TABLE transfer_log;
