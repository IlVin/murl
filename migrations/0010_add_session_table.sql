-- +goose Up
-- +goose StatementBegin

    -- 1. Добавляем колонку session_id
    ALTER TABLE murl 
        ADD COLUMN IF NOT EXISTS session_id UUID DEFAULT NULL;
    CREATE INDEX IF NOT EXISTS idx_murl_session_id ON murl(session_id); 
    
    -- 2. Если есть бэкап — восстанавливаем данные
    DO $$
    BEGIN
        IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'murl_backup_down_0010') THEN
            -- Вставляем данные, разрешая явную запись в IDENTITY
            INSERT INTO murl (id, url, session_id) OVERRIDING SYSTEM VALUE
            SELECT id, url, session_id FROM murl_backup_down_0010
            ON CONFLICT (id) DO UPDATE SET session_id = EXCLUDED.session_id;
            -- Синхронизируем счетчик ID с максимальным значением
            PERFORM setval(pg_get_serial_sequence('murl', 'id'), COALESCE(MAX(id), 1)) FROM murl;
            -- Обновляем статистику для планировщика запросов
            ANALYZE murl;
            -- Удаляем временный бэкап
            DROP TABLE murl_backup_down_0010;
        END IF;
    END $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

    -- 1. Создаем бекап users только если таблица users существует
    DROP TABLE IF EXISTS murl_backup_down_0010;
    DO $$
    BEGIN
        IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'murl' AND column_name = 'session_id') THEN
            CREATE TABLE murl_backup_down_0010 AS
            SELECT * FROM murl WHERE session_id IS NOT NULL;
        END IF;
    END $$;
    
    -- 2. Удаляем индекс из murl
    DROP INDEX IF EXISTS idx_murl_session_id;
    
    -- 3. Удаляем колонку из murl (связь FK удалится автоматически вместе с колонкой)
    ALTER TABLE murl 
        DROP COLUMN IF EXISTS session_id;

-- +goose StatementEnd