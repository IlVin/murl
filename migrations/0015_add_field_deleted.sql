-- +goose Up
-- +goose StatementBegin

    -- 1. Добавляем колонку
    ALTER TABLE murl ADD COLUMN IF NOT EXISTS deleted boolean DEFAULT false;
    
    -- 2. Частичный индекс
    CREATE INDEX IF NOT EXISTS idx_murl_deleted_true ON murl(id) WHERE (deleted IS TRUE); 
    
    -- 3. Восстановление данных
    DO $$
    BEGIN
        IF EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'murl_backup_down_0015') THEN
            -- Обновляем только те записи, которые помечены как удаленные в бэкапе
            UPDATE murl m
            SET deleted = b.deleted
            FROM murl_backup_down_0015 b
            WHERE m.id = b.id;

            DROP TABLE murl_backup_down_0015;
            ANALYZE murl;
        END IF;
    END $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

    -- 1. Бэкапим ТОЛЬКО удаленные записи (экономим место)
    DROP TABLE IF EXISTS murl_backup_down_0015;
    DO $$
    BEGIN
        IF EXISTS (SELECT FROM information_schema.columns WHERE table_name = 'murl' AND column_name = 'deleted') THEN
            CREATE TABLE murl_backup_down_0015 AS
            SELECT id, deleted FROM murl WHERE deleted = true;
        END IF;
    END $$;
    
    DROP INDEX IF EXISTS idx_murl_deleted_true;
    
    ALTER TABLE murl DROP COLUMN IF EXISTS deleted;

-- +goose StatementEnd
