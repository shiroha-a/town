-- +goose Up
-- ストリートファイト(レガシー game.cgi mode=battle)のモンスターマスタ。
--
-- レガシーは dat_dir/monsuter.cgi に1行1体を <> 区切りで持っていた。列は
-- レベル・名前・勝ち金額・負け金額・16能力・アイテム入手率・アイコン・景品商品。
-- 景品はマスタの商品を指すので、ここでは content_items への参照にする。
CREATE TABLE battle_monsters (
    id             BIGSERIAL PRIMARY KEY,
    name           TEXT    NOT NULL,
    level          INT     NOT NULL DEFAULT 1,
    -- 勝つと奪える額 / 負けると奪われる額(レガシー kachi_kingaku / make_kingaku)。
    win_money      BIGINT  NOT NULL DEFAULT 0 CHECK (win_money >= 0),
    lose_money     BIGINT  NOT NULL DEFAULT 0 CHECK (lose_money >= 0),
    -- 16能力。プレイヤーのパラメータと同じキーで持つ(effects.AllParams のうち
    -- energy/nou_energy/satiety を除いたもの)。
    params         JSONB   NOT NULL DEFAULT '{}'::jsonb,
    -- 景品のドロップ率。1/item_rate で当たる(0=景品なし。レガシー aitemuget_ritu)。
    item_rate      INT     NOT NULL DEFAULT 0 CHECK (item_rate >= 0),
    reward_item_id BIGINT  REFERENCES content_items(id) ON DELETE SET NULL,
    -- 表示アイコン(/img/svg/<icon>.svg)。
    icon           TEXT    NOT NULL DEFAULT 'slime',
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- レガシー dat_dir/monsuter.cgi の3体。景品「お鍋のふた」系は商品マスタに無いので
-- 景品なし(item_rate=0)で入れ、運用で管理画面から差し替えられるようにしておく。
--
-- スライムギャンブラーは元データの美術が 5000000000 になっている。頭脳パワー上限が
-- 8億を超えて誰も倒せなくなるため、他の能力に揃えて5にする(入力ミスとみなす)。
INSERT INTO battle_monsters (name, level, win_money, lose_money, params, item_rate, icon) VALUES
  ('スライム', 1, 10, 1,
   '{"kokugo":5,"suugaku":5,"rika":5,"syakai":5,"eigo":5,"ongaku":5,"bijutsu":5,
     "looks":5,"tairyoku":5,"kenkou":5,"speed":5,"power":5,"wanryoku":5,"kyakuryoku":5,
     "love":5,"omoshirosa":5}'::jsonb, 10, 'slime'),
  ('スライムギャンブラー', 1, 10, 1,
   '{"kokugo":5,"suugaku":5,"rika":5,"syakai":5,"eigo":5,"ongaku":5,"bijutsu":5,
     "looks":5,"tairyoku":5,"kenkou":5,"speed":5,"power":5,"wanryoku":5,"kyakuryoku":5,
     "love":5,"omoshirosa":5}'::jsonb, 10, 'slime'),
  ('スライム2', 2, 100, 100,
   '{"kokugo":25,"suugaku":25,"rika":25,"syakai":25,"eigo":25,"ongaku":25,"bijutsu":25,
     "looks":25,"tairyoku":25,"kenkou":25,"speed":25,"power":25,"wanryoku":25,"kyakuryoku":25,
     "love":25,"omoshirosa":25}'::jsonb, 10, 'slime');

-- +goose Down
DROP TABLE battle_monsters;
