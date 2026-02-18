-- +goose Up
-- +goose StatementBegin
-- 1. Резервное копирование данных
DROP TABLE IF EXISTS murl_backup_up_0005;
CREATE TABLE murl_backup_up_0005 AS 
SELECT * FROM murl WHERE LENGTH(url) > 8192;

-- 2. Удаляем длинные записи
DELETE FROM murl WHERE LENGTH(url) > 8192;

-- 3. Применяем изменения (VARCHAR(8192) и NOT NULL)
ALTER TABLE murl 
    ALTER COLUMN url TYPE VARCHAR(8192),
    ALTER COLUMN url SET NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ 
DECLARE
    back_table_exists boolean;
    rows_recovered int;
BEGIN
    -- 1. Возвращаем тип данных к TEXT
    ALTER TABLE murl 
        ALTER COLUMN url TYPE TEXT,
        ALTER COLUMN url SET NOT NULL;

    -- 2. Проверка существования таблицы (добавлена проверка текущей схемы)
    SELECT EXISTS (
        SELECT FROM information_schema.tables 
        WHERE table_name = 'murl_backup_up_0005'
        AND table_schema = current_schema()
    ) INTO back_table_exists;

    -- 3. Восстановление данных
    IF back_table_exists THEN
        INSERT INTO murl
        SELECT * FROM murl_backup_up_0005
        ON CONFLICT (url) DO NOTHING;
        
        GET DIAGNOSTICS rows_recovered = ROW_COUNT;
        RAISE NOTICE 'Restored % rows from backup table', rows_recovered;

        DROP TABLE murl_backup_up_0005;
    ELSE
        RAISE NOTICE 'Backup table murl_backup_up_0005 not found, skipping recovery';
    END IF;
END $$;
-- +goose StatementEnd
