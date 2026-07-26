-- +goose Up

-- アイテムの耐久・在庫・使用時消費・カロリー・購入上限(レガシー syouhin.cgi 準拠)。
-- durability      = 1セットあたりの耐久(旧 syo_taikyuu)
-- durability_unit = 'use'(使用ごとに残回数-1) / 'day'(購入日からの残日数を日次で-1)
-- stock_master    = マスタ在庫(日次補充の元、旧 syo_zaiko)。NULL=在庫無制限(サービス等)
-- body_cost/nou_cost = 使用時の身体/頭脳パワー消費(旧 syo_sintai_syouhi/syo_zunou_syouhi)
-- calorie_g       = カロリー=体重増加量(g)。旧 syo_cal/1000 kg を g で保持
-- max_sets        = 同一アイテムの最大所持セット数(旧 item_kosuuseigen)
ALTER TABLE content_items
  ADD COLUMN durability      INT  NOT NULL DEFAULT 1,
  ADD COLUMN durability_unit TEXT NOT NULL DEFAULT 'use' CHECK (durability_unit IN ('use', 'day')),
  ADD COLUMN stock_master    INT,
  ADD COLUMN body_cost       INT  NOT NULL DEFAULT 0,
  ADD COLUMN nou_cost        INT  NOT NULL DEFAULT 0,
  ADD COLUMN calorie_g       INT  NOT NULL DEFAULT 0,
  ADD COLUMN max_sets        INT  NOT NULL DEFAULT 5;

-- 持ち物の残量。'use'なら残り総使用回数、'day'なら残り日数。
-- 旧の「持ち物の耐久フィールド=残り総使用回数(セット数×1セット耐久の積算)」に相当。
ALTER TABLE player_items
  ADD COLUMN remaining_uses INT NOT NULL DEFAULT 0;

-- 既存の持ち物を残量へ移行(この時点で durability=1 のため remaining_uses = quantity)。
UPDATE player_items pi
SET remaining_uses = pi.quantity * ci.durability
FROM content_items ci
WHERE pi.item_id = ci.id;

-- 店頭在庫(施設×アイテム×ゲーム内日付)。日境界(AM5:00)ごとにアクション時lazy生成される。
CREATE TABLE shop_daily_stock (
    facility  TEXT   NOT NULL,
    item_id   BIGINT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
    game_date DATE   NOT NULL,
    remaining INT    NOT NULL,
    PRIMARY KEY (facility, item_id, game_date)
);

-- +goose Down
DROP TABLE shop_daily_stock;
ALTER TABLE player_items DROP COLUMN remaining_uses;
ALTER TABLE content_items
  DROP COLUMN durability,
  DROP COLUMN durability_unit,
  DROP COLUMN stock_master,
  DROP COLUMN body_cost,
  DROP COLUMN nou_cost,
  DROP COLUMN calorie_g,
  DROP COLUMN max_sets;
