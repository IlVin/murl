## Сервисный слой (Module: service)

Модуль `service` является ядром бизнес-логики приложения. Он выполняет роль связующего звена (оркестратора) между транспортным уровнем (HTTP), хранилищем данных (Repository) и алгоритмами кодирования (Model).

### Основные функции

*   **Валидация входящих URL:** Строгая проверка входящих строк на соответствие формату URL, наличие протоколов (`http`/`https`) и абсолютность пути.
*   **Оркестрация потоков данных:** Координация процесса сохранения длинных ссылок и извлечения оригинальных адресов по коротким идентификаторам.
*   **Dependency Injection (DI):** Сервис полностью отвязан от конкретных реализаций логгера и базы данных через интерфейсы `ServiceConfig` и `MicroURLRepo`. Это обеспечивает высокую тестируемость и легкость замены компонентов.
*   **Высокопроизводительное логирование:** Использование нативного API `slog` с типизированными полями гарантирует минимальные задержки при записи системных событий.

### Схема оркестрации (Data Flow)

Ниже представлена схема обработки запроса на сокращение ссылки:

```mermaid
sequenceDiagram
    participant API as HTTP Handler
    participant SVC as Service Layer
    participant STR as MicroURLRepo (DB)
    participant MDL as Model (Encoding)

    API->>SVC: AddURL(longURL)
    Note over SVC: Валидация схемы и формата
    SVC->>STR: Save(longURL)
    STR-->>SVC: shardID, index, err
    SVC->>MDL: MakeShortURL(shardID, index, baseURL)
    MDL-->>SVC: shortURL, err
    Note over SVC: Логирование результата (slog)
    SVC-->>API: shortURL, err
