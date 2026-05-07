# MicroURL

![coverage](https://raw.githubusercontent.com/IlVin/murl/badges/.badges/23/merge/coverage.svg)

HighLoad сервис сокращателя ссылок.  
Преобразует длинные URL (**LongURL**) в короткие (**ShortURL**) и обратно.

Так как сервис проектируется под огромные нагрузки, то HighLoad архитектура сервиса влияет на формат **ShortURL**.

## Формат ShortURL

    [proto]://[domain]/[ShardID][LongUrlID]

**\[ShardID]** (1 символ) - Идентификатор шарда, на котором хранится LongURL в формате BASE64u  
**\[LongUrlID]** (остальные символы) - Идентификатор LongURL записи в шарде в формате BASE64u

**\[ShardID]** и **\[LongUrlID]** кодируются в **BASE64u** потому, что символы этого формата не подлежат URL эскейпингу.

```mermaid
graph LR
    subgraph "ShortURL Format"
    A[https://] --- B[micro.url/]
    B --- C{{"ShardID"}}
    C --- D{{"LongUrlID"}}
    end

    style C fill:#f96,stroke:#333,stroke-width:2px
    style D fill:#6cf,stroke:#333,stroke-width:2px

    C -.-> C1["1 символ (BASE64u)<br/>Определяет БД (0-63)"]
    D -.-> D1["N символов (BASE64u)<br/>Уникальный ID в шарде"]
```

## Шардирование

Для распределения нагрузки по нескольким серверам используется горизонтальное масштабирование,
которое подразумевает разбиение всего массива данных на несколько отдельных независимых частей, в
общем случае хранящихся в отдельных инстансах БД.

Так как для номера шарда **ShardID** выделен 1 символ из набора BASE64u, то максимальный объем шардированного хранилища ограничен 64 шардами.

```mermaid
graph TD
    %% Основной массив данных
    DataStream[(Весь массив LongURL)] --> Balancer{"Алгоритм распределения ShardID = MurmurHash3(LongURL) % 64"}

    %% Процесс распределения
    subgraph "Шардированное хранилище (Max: 64 инстанса)"
        Shard0[(Shard 0 <br/> 'A')]
        Shard1[(Shard 1 <br/> 'B')]
        ShardDot[ . . . ]
        Shard63[(Shard 63 <br/> '-')]
    end

    %% Связи
    Balancer -- "ShardID: 0" --> Shard0
    Balancer -- "ShardID: 1" --> Shard1
    Balancer -- "ShardID: 63" --> Shard63

    %% Пояснение про BASE64u
    subgraph "Логика адресации (BASE64u)"
        Note1[1 символ ShardID = 6 бит]
        Note2[2^6 = 64 возможных значения]
    end

    Note1 -.-> Balancer
    Note2 -.-> Shard63

    %% Стилизация
    style DataStream fill:#e1f5fe,stroke:#01579b
    style Balancer fill:#fff4dd,stroke:#d4a017
    style Shard0 fill:#f1f8e9,stroke:#33691e
    style Shard1 fill:#f1f8e9,stroke:#33691e
    style Shard63 fill:#f1f8e9,stroke:#33691e
```

### Формула вычисления номера шарда

    ShardID = MurmurHash3(LongURL) % 64

Функция хэширования **MurmurHash3** выбрана потому, что она быстро вычисляется и обеспечивает разницу менее 1% в объеме данных между
самым «тяжелым» и «легким» шардами, что исключает появление «горячих» шардов.

## Роутинг по шардам

Явное указание номера шарда в ShortURL имеет:  
+ **Плюс**: Можно маршрутизировать запрос в шард не только на уровне **Приложение->БД**, но и на уровне **L7 Роутер->Приложение** с наименьшими затратами вычислительных ресурсов.
+ **Минус**: Номер шарда захардкожен, поэтому добавление новых шардов к имеющемуся пулу не снизит в короткой перспективе нагрузку на уже имеющиеся шарды.

```mermaid
graph TD
    User((Пользователь)) -- "GET /aB12" --> Router{L7 Router / Nginx}

    subgraph "Уровень Приложений (Routing Logic)"
        Router -- "ShardID='a' (Быстрый парсинг)" --> App_A[App Group A]
        Router -- "ShardID='b'" --> App_B[App Group B]
        Router -- "ShardID='...'" --> App_N[App Group N]
    end

    subgraph "Шардированная БД (Static Pool)"
        App_A --> Shard_A[(Shard 'a')]
        App_B --> Shard_B[(Shard 'b')]
        
        subgraph "Проблема масштабирования (Минус)"
            NewShard[(New Shard '!')]
            Note[Новый шард простаивает для <br/>старых ShortURL, так как <br/>их ShardID захардкожен]
            NewShard -.-> Note
        end
    end

    %% Акценты на Плюсы и Минусы
    classDef plus fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px;
    classDef minus fill:#ffebee,stroke:#c62828,stroke-width:2px;

    P[ПЛЮС: Мгновенный L7-роутинг <br/>без обращения к Config Service]:::plus
    M[МИНУС: Старые ссылки <br/>привязаны к своим шардам навечно]:::minus

    Router -.-> P
    Note -.-> M

```

## Горизонтальное масштабирование по инстансам

Для нивелирования указанного минуса нужно завести пул из максимально возможного количества шардов и распределить его на имеющиеся инстансы, в общем случае не равномерно с учетом вычислительной мощности каждого инстанса.

Когда инстансы перестанут справляться с нагрузкой, можно будет добавить еще несколько инстансов и перераспределить имеющиеся 64 БД на уже увеличенное количество инстансов.

## Горизонтальное масштабирование по шпинделям

В базах данных помимо вычислительных ресурсов CPU активно утилизируется пропускная способность HDD. Наличие нескольких шардов на одном инстансе БД PostgreSQL (каждый в отдельной DATABASE/TABLESPACE) позволяет сделать оптимизацию, когда каждый шард находится на выделенном HDD. При таком решении пропускные способности "шпинделей" инстанса складываются. Такое решение было довольно эффективно в эру HDD.

## Горизонтальное масштабирование CQRS

**CQRS (Comand Query Resposibility Segregation)** - разделение операций чтения и записи данных в приложении.
Сервис **MircoURL** неравномерно нагружен по операциям записи и чтения: 1% нагрузки - это запись **LongURL** в БД и 99% нагрузки - это чтение **LongURL** по **ShortURL** идентификатору.

Для таких **read-heavy** систем БД применяется паттерн проектирования **CQRS**, суть которого заключается в назначении одного из инстансов БД RW мастером с возможностью записи новых данных в его шарды и копировании данных с помощью репликации с ведущего RW истанса в несколько ведомых RO истансов. А приложение, работающее с хранилищем, из конфигурации определяет в какой инстанс БД можно **INSERT**ить новые данные, а из какого инстанса **SELECT**ить.

## Ограничения PostgreSQL

В БД PostgreSQL есть известная проблема: соединение с БД очень дорогое. Поэтому количество одновременно работающих соединений в PostgreSQL ограничено сотнями соединений.

Для нивелирования этой проблемы используют Proxy серверы, например: PgPool II. Такие решения решают проблему множественных соединений к БД, но при этом вносят свои существенные особенности в работу с БД.

Но мы пойдем другим путем: каждое приложение будет иметь одно соединение к шарду в инстансе. Количество соединений к PostgreSQL инстансу от запущенных приложений регулируется количеством шардов, расположенных на инстансе. Т.е., если мы приблизились к пределу количества коннектов к инстансу PostgreSQL, нужно добавить инстансов и перераспределить шарды на новые инстансы. Количество соединений на каждом инстансе уменьшится. Это решение неприемлемо для энтерпрайзных решений, но для микросерверной архитектуры, когда для каждого микросервиса выделяется своя БД, решение приемлемо.


## Резервное копирование и восстановление после сбоев

Репликация помимо распределения RW и RO нагрузки на разные инстансы дает возможность резервного копирования данных без влияния на функциональность всего сервиса и возможность быстрого переключения со сломанного RW инстанса на исправный RO инстанс с одновременным переводом его в роль RW инстанса. Если добавить в систему оркестратор кластера типа **Patroni**, то такое переключение будет осуществляться автоматичеки. Но кластерное решение по отказоустойчивости выходит за рамки моего учебного проекта.

```mermaid
graph TD
    subgraph "Уровень Трафика (99% Read / 1% Write)"
        RW_Traffic[Write Traffic] -- "1%" --> App_Write[API Write Nodes]
        RO_Traffic[Read Traffic] -- "99%" --> App_Read[API Read Nodes]
    end

    subgraph "Инстанс БД (Масштабирование по инстансам)"
        direction TB
        subgraph "Primary Node (RW)"
            Master_DB[(Master PG)]
            
            subgraph "Масштабирование по шпинделям"
                Shard_1[[Shard 1: DB/Tablespace]] --- HDD1[(Dedicated HDD 1)]
                Shard_2[[Shard 2: DB/Tablespace]] --- HDD2[(Dedicated HDD 2)]
            end
        end

        subgraph "Replica Nodes (RO)"
            Replica_1[(Replica 1)]
            Replica_2[(Replica 2)]
            Replica_N[(Replica N)]
        end
    end

    %% Потоки данных
    App_Write -- "Запись" --> Master_DB
    Master_DB -- "Репликация" --> Replica_1
    Master_DB -- "Репликация" --> Replica_2
    Master_DB -- "Репликация" --> Replica_N
    
    App_Read -- "Чтение" --> Replica_1
    App_Read -- "Чтение" --> Replica_2

    %% Стили
    style Master_DB fill:#ffebee,stroke:#c62828
    style Replica_1 fill:#e8f5e9,stroke:#2e7d32
    style Replica_2 fill:#e8f5e9,stroke:#2e7d32
    style Replica_N fill:#e8f5e9,stroke:#2e7d32
    style Shard_1 fill:#fff3e0,stroke:#ef6c00
    style Shard_2 fill:#fff3e0,stroke:#ef6c00
```

## Кэширование

Так как наиболее производительной репликацией является асинхронная репликация, то сервис будет страдать рассинхронизацией данных, когда данные записываются на RW сервер, но не успевают скопироваться на RO сервера (Stanby) до момента чтения данных с RO серверов.

Нивелировать этот эффект может применение очень быстрых InMemory кэшей данных, например Redis.

**Запись длинного URL в БД:**
+ App записывает LongURL в RW БД и получает ShortURL
+ App записывает связь ShortURL => LongURL в Redis с приемлемым TTL (3600 сек, например)
+ App публикует ShortURL пользователю

**Чтение длинного URL из БД:**
+ App запрашивает ShortURL в Redis
+ App, если ShortURL не нашелся в Redis, запрашивает ShortURL в RO БД.
+ Опционально: App записывает связь ShortURL => LongURL в Redis с приемлемым TTL (3600 сек, например)

В результате пользователь будет получать свою длинную ссылку из Redis пока данные реплицируются между RW и RO базами данных.

Конечно, бывают случаи, когда репликация задерживается на очень продолжительный срок, но будем относить такие случаи к аварийным событиям.  
Очень хочется при отсутствии записи в БД сходить в RW или другую RO БД, но этого делать нельзя, так как это путь к лавинообразному возрастанию паразитной нагрузки. Лучше пользователю честно сообщить о том, что URL не найден.

```mermaid

sequenceDiagram
    autonumber
    participant User as Пользователь
    participant App as API Service
    participant Redis as Redis (In-Memory)
    participant RW as DB Primary (RW)
    participant RO as DB Replica (RO)

    Note over User, RO: Процесс записи (Write Path)
    User->>App: POST LongURL
    App->>RW: Запись LongURL
    RW-->>App: Возврат ShortURL
    App->>Redis: SET ShortURL => LongURL (TTL 3600s)
    Note right of Redis: Данные доступны мгновенно
    App-->>User: ShortURL выдан

    Note over User, RO: Процесс чтения (Read Path - сразу после записи)
    User->>App: GET ShortURL
    App->>Redis: GET ShortURL
    
    alt В кэше найдено
        Redis-->>App: LongURL (Мгновенно)
        App-->>User: 301 Redirect
    else В кэше не найдено (Miss)
        App->>RO: SELECT LongURL
        Note right of RO: Репликация может еще <br/>не завершиться!
        RO-->>App: LongURL
        App->>Redis: SET ShortURL (Cache Fill)
        App-->>User: 301 Redirect
    end

    Note over RW, RO: Асинхронная репликация (Задержка 50-500ms)
    RW-->>RO: Replication Sync
```

## Достоверность ShortURL

Некоторые пользователи будут просить MicroURL выдать LongURL для несуществующих ShortURL. Для противодействия таким действиям ShortURL должен содержать контрольные суммы, верифицирующие с некоторой долей вероятности достоверность данных, зашитых в ShortURL.

## Секционирование

Так как сервис планируется высоконагруженным, то и данных пользователей в нем будет довольно много.
Чтобы снизить нагрузку на БД во время удаления устаревших данных (**DROP PARTITION**), меньше страдать от **AUTOVACUUM** и модификации схемы хранения данных (**ALTER TABLE**), применяют вертикальное секционирование таблиц.

В моем учебном проекте я применяю Range - секционирование таблиц по времени создания записи.
Чтобы уменьшить индекс, время буду гранулировать по номеру дня (номер диапазона времени в 86400 сек) с 1 января 2026 года.
Для хранения времени использую самый маленький тип данных PostgreSQL **SMALLINT** (2 байта).
Побочный эффект: TTL длинных URL будет кратно 1 дню.

```mermaid

graph TD
    subgraph "Логическая таблица: urls_shard_N"
        Table[Master Table: urls]
    end

    subgraph "Секции (Partition by Range: created_day)"
        P1["p2026_01_01<br/>(Day 0)"]
        P2["p2026_01_02<br/>(Day 1)"]
        P3["p2026_01_03<br/>(Day 2)"]
        P_Next[...]
        P_Old["p_expired"]
    end

    %% Связи
    Table --> P1
    Table --> P2
    Table --> P3
    Table --> P_Next

    %% Операции
    Admin((Maintenance)) -- "DROP PARTITION" --> P_Old
    
    %% Описание типа данных
    subgraph "Оптимизация (2026)"
        Note1["created_day: SMALLINT (2 bytes)"]
        Note2["Value = (Current_Date - 2026-01-01) / 86400"]
    end

    Note1 -.-> Table
    Note2 -.-> Table

    %% Стилизация
    style Table fill:#e1f5fe,stroke:#01579b,stroke-width:2px
    style P1 fill:#f1f8e9,stroke:#33691e
    style P2 fill:#f1f8e9,stroke:#33691e
    style P3 fill:#f1f8e9,stroke:#33691e
    style P_Old fill:#ffebee,stroke:#c62828,stroke-dasharray: 5 5
```


## Схема базы данных

В каждом из 64 шардов создается таблица, секционированная по времени.
Логическая структура (DDL для одного шарда):

```SQL
    CREATE TABLE murl (
        id         BIGINT NOT NULL,
        long_url   TEXT NOT NULL,
        created_day SMALL INTEGER GENERATED ALWAYS AS (
            FLOOR(EXTRACT(EPOCH FROM (created_day - '2025-01-01 00:00:00+00')) / 86400)
        ) STORED,
        CONSTRAINT pk_murl PRIMARY KEY (id, created_day)
    )
    PARTITION BY LIST (created_day);

    CREATE TABLE murl_day_377 PARTITION OF murl
        FOR VALUES IN (377);

    CREATE TABLE murl_day_378 PARTITION OF murl
        FOR VALUES IN (378);
    ...
```

```mermaid

graph TD
    subgraph "Логическая таблица (Parent Table)"
        MURL["<b>TABLE murl</b><br/>(Master Interface)"]
    end

    subgraph "Схема хранения (Data Layout)"
        direction LR
        ID["<b>id</b><br/>BIGINT<br/>(Part of PK)"]
        LURL["<b>long_url</b><br/>TEXT"]
        CDAY["<b>created_day</b><br/>SMALL INTEGER (STORED)<br/><i>Hash/List Key</i>"]
    end

    MURL --- ID
    MURL --- LURL
    MURL --- CDAY

    subgraph "Секции (Physical Storage)"
        direction TB
        P377["<b>murl_day_377</b><br/>FOR VALUES IN (377)"]
        P378["<b>murl_day_378</b><br/>FOR VALUES IN (378)"]
        P_Next["...<br/>murl_day_N"]
    end

    %% Логика вычисления
    Formula{{"Генерация created_day"}}
    Formula -.->|Formula| CDAY
    Note["Epoch from 2025-01-01 / 86400<br/>(Кол-во дней с начала эпохи)"] -- "Calculates" --> Formula

    %% Связь с секциями
    CDAY ==>|Routing| P377
    CDAY ==>|Routing| P378
    CDAY ==>|Routing| P_Next

    %% Стилизация
    style MURL fill:#e1f5fe,stroke:#01579b,stroke-width:2px
    style CDAY fill:#fff3e0,stroke:#ef6c00
    style P377 fill:#f1f8e9,stroke:#33691e
    style P378 fill:#f1f8e9,stroke:#33691e
    style Formula fill:#f3e5f5,stroke:#7b1fa2
```

## Обслуживание секционированной таблицы

**Секционированные таблицы требует обслуживания: нужно вовремя создавать партиции на следующие N дней.**  
На шарде обслуживать секционированные таблицы силами DBA довольно затруднительно, поэтому следует
применить расширение **pg_cron**, которое будет создавать новые партиции.  

```SQL
    SELECT cron.schedule('create-next-partitions-murl', '0 23 * * *', $$
    DO $do$
    DECLARE
        -- Укажите здесь количество дней, на которое нужно создать секции вперед
        days_ahead int := 3; 
        
        current_day_id int;
        target_day_id int;
        target_table_name text;
    BEGIN
        -- Вычисляем ID текущего дня (относительно 2025-01-01)
        current_day_id := FLOOR(EXTRACT(EPOCH FROM (NOW() - '2025-01-01'::timestamp)) / 86400);

        FOR i IN 1..days_ahead LOOP
        target_day_id := current_day_id + i;
        target_table_name := 'murl_day_' || target_day_id;

        IF NOT EXISTS (SELECT FROM pg_tables WHERE tablename = target_table_name) THEN
            EXECUTE format('CREATE TABLE %I PARTITION OF murl FOR VALUES IN (%L)', 
                        target_table_name, target_day_id);
        END IF;
        END LOOP;
    END $do$;
    $$);
```  

Но в моем учебном проекте **pg_cron** будет и удалять старые, так как я ленивый и в данном случае безответственный (у учебного проекта нет ответственности)...  

```SQL
    -- Задание: раз в месяц удалять секции старше 90 дней
    SELECT cron.schedule('purge-old-partitions-murl', '0 0 1 * *', $$
    DO $purge$
    DECLARE
        row record;
        -- Определяем порог удаления: сегодняшний номер дня минус 90
        retention_threshold int := FLOOR(EXTRACT(EPOCH FROM (NOW() - '2025-01-01')) / 86400) - 90;
        partition_value int;
    BEGIN
        -- Цикл по всем секциям родительской таблицы 'murl'
        FOR row IN
            SELECT
                nmsp_parent.nspname AS parent_schema,
                parent.relname      AS parent_name,
                nmsp_child.nspname  AS child_schema,
                child.relname       AS child_name
            FROM pg_inherits
                JOIN pg_class parent            ON pg_inherits.inhparent = parent.oid
                JOIN pg_class child             ON pg_inherits.inhrelid  = child.oid
                JOIN pg_namespace nmsp_parent   ON parent.relnamespace   = nmsp_parent.oid
                JOIN pg_namespace nmsp_child    ON child.relnamespace    = nmsp_child.oid
            WHERE parent.relname = 'murl'
        LOOP
            -- Извлекаем числовой ID дня из имени таблицы (например, из 'murl_377' получим 377)
            -- Используем регулярное выражение для поиска цифр в конце имени
            partition_value := (regexp_matches(row.child_name, '(\d+)$'))[1]::int;

            -- Если номер дня в названии секции меньше порога — удаляем
            IF partition_value < retention_threshold THEN
                RAISE NOTICE 'Удаление старой секции: %', row.child_name;
                EXECUTE format('DROP TABLE %I.%I', row.child_schema, row.child_name);
            END IF;
        END LOOP;
    END $purge$;
    $$);
```

## Инфраструктура

Разработчик может легко развернуть описанный кластер MicroURL сервиса по [инструкции](infra/pg18/README.md)
