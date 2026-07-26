-- 住民のデータだけを全消しし、街(マップ)と管理者が作ったコンテンツは残す。
--
--   docker compose -f deploy/compose.yaml exec -T postgres \
--     psql -U town -d town -v ON_ERROR_STOP=1 -f /dev/stdin < deploy/reset_user_data.sql
--
-- 残すものだけを列挙し、それ以外は全部消す。テーブルが増えたときに消し忘れる
-- (=個人データが残る)より、消しすぎて作り直す方が安全なため。
--
-- players を消すだけでは足りない: player_houses と saisen_log は NO ACTION で
-- 削除を拒否し、greetings / house_bbs / messages / town_news は SET NULL なので
-- 投稿本文が作者不明のまま残ってしまう。
DO $$
DECLARE
    keep text[] := ARRAY[
        -- スキーマ管理
        'goose_db_version',
        -- 管理者が設定するもの
        'app_settings', 'town_map', 'uploaded_images', 'instance_rules',
        'content_items', 'content_jobs', 'content_events', 'serial_codes',
        -- 街そのものの状態(住民に紐づかない)
        'stock_price', 'stock_event_log',
        -- 外部から取得したカスタム絵文字のキャッシュ(住民に紐づかない)
        'misskey_emojis', 'misskey_emoji_rejects'
    ];
    victims text;
BEGIN
    SELECT string_agg(format('%I', tablename), ', ')
      INTO victims
      FROM pg_tables
     WHERE schemaname = 'public' AND NOT (tablename = ANY (keep));

    IF victims IS NULL THEN
        RAISE NOTICE '消すテーブルがありません';
        RETURN;
    END IF;

    RAISE NOTICE '消去: %', victims;
    EXECUTE format('TRUNCATE %s RESTART IDENTITY CASCADE', victims);
END $$;

-- 消した後の確認用。
SELECT 'players' AS t, count(*) FROM players
UNION ALL SELECT 'town_map(残す)', count(*) FROM town_map
UNION ALL SELECT 'content_items(残す)', count(*) FROM content_items
UNION ALL SELECT 'app_settings(残す)', count(*) FROM app_settings
ORDER BY 1;
