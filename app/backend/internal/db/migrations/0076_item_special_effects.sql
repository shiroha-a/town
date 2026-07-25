-- +goose Up
-- レガシーの特殊効果(basic0.cgi の $syo_kouka)をアイテムデータへ反映する。
-- 効果エンジンに add_weight_g / add_height_cm / add_disease を追加したのに合わせ、
-- 該当アイテムの effect に op を追記する(既存のパラメータ効果は残す)。
-- レガシー対応: ウエイトアップ/ダイエット→体重(kg→g)、身長/縮み→身長(cm)、
--               万能→病気指数を無条件回復、風邪/下痢/肺炎/結核→その病気のときだけ回復。

UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 1000}]'::jsonb
  WHERE name = 'ウエイトアップケーキ' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 1000}]'::jsonb
  WHERE name = 'ウエイトアップバーガー' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 2000}]'::jsonb
  WHERE name = 'ウエイトアッププロテイン' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,2
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 1000}]'::jsonb
  WHERE name = 'ウエイトアップミルク' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 4000}]'::jsonb
  WHERE name = 'ウェイトアップ器具' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,4
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 20}]'::jsonb
  WHERE name = 'おばあちゃんの知恵薬（スーパー）' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 万能,20
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 15}]'::jsonb
  WHERE name = 'おばあちゃんの知恵薬' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 万能,15
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": -3}]'::jsonb
  WHERE name = 'キティのパジャマ' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 縮み,3
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": -4000}]'::jsonb
  WHERE name = 'キティのハンカチ' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ダイエット,4
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": 3}]'::jsonb
  WHERE name = 'キティの寝具セット' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 身長,3
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 4000}]'::jsonb
  WHERE name = 'キティの着ぐるみ' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,4
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": 5}]'::jsonb
  WHERE name = 'キリン' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 身長,5
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": -2000}]'::jsonb
  WHERE name = 'スリムアップビューティ' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ダイエット,2
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": 1}]'::jsonb
  WHERE name = 'セイ・ノビール' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 身長,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": -1000}]'::jsonb
  WHERE name = 'ダイエットお茶' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ダイエット,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": -1000}]'::jsonb
  WHERE name = 'ダイエットケーキ' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ダイエット,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": -1000}]'::jsonb
  WHERE name = 'ダイエットバーガー' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ダイエット,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": -2000}]'::jsonb
  WHERE name = 'ダイエット茶' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ダイエット,2
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 7, "disease": "風邪"}]'::jsonb
  WHERE name = 'タフマン(ギフト)' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 風邪,7,ギフト
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": -1}]'::jsonb
  WHERE name = 'チヂミ' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 縮み,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": -1000}]'::jsonb
  WHERE name = 'ドラゴンクファンタジー' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ダイエット,1
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 3}]'::jsonb
  WHERE name = 'ユンケル' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 万能,3
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 2}]'::jsonb
  WHERE name = 'リコリス' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 万能,2
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 2}]'::jsonb
  WHERE name = 'リポビタンD' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 万能,2
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 1000}]'::jsonb
  WHERE name = '体重アップの秘訣' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 2000}]'::jsonb
  WHERE name = '子豚' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,2
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 18, "disease": "下痢"}]'::jsonb
  WHERE name = '強力下痢止め' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 下痢,18
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 20, "disease": "風邪"}]'::jsonb
  WHERE name = '強力風邪薬' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 風邪,20
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 40, "disease": "結核"}]'::jsonb
  WHERE name = '結核薬' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 結核,40
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 20, "disease": "肺炎"}]'::jsonb
  WHERE name = '肺炎に効く薬' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 肺炎,20
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": 1}]'::jsonb
  WHERE name = '背が伸びる薬' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 身長,1
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": -1}]'::jsonb
  WHERE name = '背が縮む薬' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 縮み,1
UPDATE content_items SET effect = effect || '[{"op": "add_weight_g", "amount": 10000}]'::jsonb
  WHERE name = '豚' AND NOT effect @> '[{"op": "add_weight_g"}]'::jsonb;  -- ウエイトアップ,10
UPDATE content_items SET effect = effect || '[{"op": "add_height_cm", "amount": 1}]'::jsonb
  WHERE name = '身長アップ器具' AND NOT effect @> '[{"op": "add_height_cm"}]'::jsonb;  -- 身長,1
UPDATE content_items SET effect = effect || '[{"op": "add_disease", "amount": 10, "disease": "風邪"}]'::jsonb
  WHERE name = '風邪薬' AND NOT effect @> '[{"op": "add_disease"}]'::jsonb;  -- 風邪,10

-- +goose Down
UPDATE content_items SET effect = COALESCE((
  SELECT jsonb_agg(e) FROM jsonb_array_elements(effect) e
  WHERE e->>'op' NOT IN ('add_weight_g', 'add_height_cm', 'add_disease')
), '[]'::jsonb)
WHERE effect @> '[{"op":"add_weight_g"}]'::jsonb
   OR effect @> '[{"op":"add_height_cm"}]'::jsonb
   OR effect @> '[{"op":"add_disease"}]'::jsonb;
