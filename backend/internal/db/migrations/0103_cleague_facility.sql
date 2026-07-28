-- +goose Up
-- Cリーグ会場を街の施設として置く。
--
-- レガシーは街マップに置ける施設(unit.pl の「キャラ」= chara_battle.gif、
-- 「最強のキャラクターを決める『Cリーグ』会場です。」)から入る作りだった。
-- リライトはコマンドバーのボタンからしか行けなかったので、既定の街(公園)の
-- 空きマス(F/G列の草地)に会場を足す。default_map.json は新規インストール用で、
-- すでに保存済みの盤面には効かないためここで足す。
UPDATE town_map
SET facilities = facilities || jsonb_build_array(jsonb_build_object(
      'key', 'doukyo', 'img', 'cleague', 'alt', 'Cリーグ会場',
      'town', 0, 'col', 5, 'row', 6, 'dest', 0, 'ready', true))
WHERE id = 1
  AND NOT facilities @> '[{"key": "doukyo"}]'::jsonb
  -- 置こうとしているマスが空いているときだけ(管理画面で別の物を置いていたら触らない)。
  AND NOT facilities @> '[{"town": 0, "col": 5, "row": 6}]'::jsonb;

-- +goose Down
UPDATE town_map
SET facilities = (
  SELECT COALESCE(jsonb_agg(f ORDER BY ord), '[]'::jsonb)
  FROM jsonb_array_elements(facilities) WITH ORDINALITY AS t(f, ord)
  WHERE NOT (f @> '{"key": "doukyo", "img": "cleague"}'::jsonb)
)
WHERE id = 1;
