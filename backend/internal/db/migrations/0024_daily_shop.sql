-- +goose Up
-- デパート/食堂の日次品揃え。商品プールから game_date をシードに決定論的な一部だけを
-- 選ぶ(旧TOWNは毎日ランダムに depart=100/syokudou=9 件を抽出して記録)。md5(id|daykey)の
-- 昇順で先頭N件を「本日の品揃え」とし、記録テーブル無しで list と buy が同じ集合に一致する。
-- +goose StatementBegin
CREATE FUNCTION daily_shop_ids(p_facility text, p_daykey text, p_n int)
RETURNS TABLE(id bigint) AS $$
  SELECT ci.id FROM content_items ci
  WHERE ci.enabled AND ci.facility = p_facility
  ORDER BY md5(ci.id::text || '|' || p_daykey)
  LIMIT p_n;
$$ LANGUAGE sql STABLE;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION IF EXISTS daily_shop_ids(text, text, int);
