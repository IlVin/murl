# Murl Middleware Stack

Набор высокопроизводительных посредников (middlewares) для Go-сервисов, обеспечивающих расширенное логирование и адаптивное сжатие трафика.

## Основные возможности

### 1. Логирование (`WithLogging`)
* **Структурированный лог**: Использует `uber-go/zap` для записи URI, метода, статуса ответа и Content-Type.
* **Сбор метрик**: Инициализирует структуру `TMetrics` в контексте, позволяя другим middleware записывать данные о размерах.
* **Тайминг**: Точное измерение времени обработки запроса (`duration`).

### 2. Сжатие и Распаковка (`WithCompress`)
* **Двустороннее сжатие**: 
    * **Ответы**: Сжимает данные (Brotli, Gzip, Deflate) на основе заголовка `Accept-Encoding` и весов `q`.
    * **Запросы**: Автоматически распаковывает `Body` входящих запросов, если указан `Content-Encoding`.
* **Оптимизация (sync.Pool)**: Использует пулы объектов для всех компрессоров и декомпрессоров, минимизируя нагрузку на GC.
* **Фильтрация**: Сжимает только разрешенные типы контента (JSON, HTML и т.д.).
* **RFC Compliance**: Корректная обработка заголовка `Vary: Accept-Encoding`.

---

## Установка

```bash
go get go.uber.org/zap
go get ://github.com
```

## Пример использования

### 1. Реализация интерфейса конфигурации

```go
type MyConfig struct {
    logger *zap.Logger
}

func (c *MyConfig) Zap() *zap.Logger { return c.logger }

func (c *MyConfig) CompressibleContentTypes() map[string]struct{} {
    return map[string]struct{}{
        "application/json": {},
        "text/html":        {},
        "text/plain":       {},
    }
}
```

### 2. Регистрация в HTTP-сервере

**Важно:** Для корректного подсчета метрик логгер должен оборачивать компрессор.

```go
func main() {
    cfg := &MyConfig{logger: zap.NewExample()}
    mux := http.NewServeMux()

    // Регистрация хендлеров
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"message": "hello world"}`))
    })

    // Построение цепочки (Middleware Chain)
    // Сначала сжатие, затем логирование поверх него
    handler := middleware.WithCompress(cfg, mux)
    handler = middleware.WithLogging(cfg, handler)

    http.ListenAndServe(":8080", handler)
}
```

## Архитектура сбора данных

Модули взаимодействуют через структуру метрик в контексте:

| Поле | Описание | Устанавливается в |
| :--- | :--- | :--- |
| **OriginalSize** | Размер данных до сжатия (исходный) | `WithCompress` |
| **ResponseSize** | Размер байт, реально отправленных в сеть | `WithLogging` |
| **IsCompressed** | Флаг, указывающий, было ли применено сжатие | `WithCompress` |

## Технические детали

*   **Zero-allocation**: Благодаря использованию `sync.Pool`, создание объектов сжатия на каждый запрос практически не аллоцирует новую память.
*   **Safety**: Входящее тело запроса подменяется через `readCloserWrapper`, что гарантирует корректное закрытие как распаковщика (с возвратом в пул), так и оригинального сетевого соединения.
*   **Brotli Quality**: По умолчанию используется **Quality: 4** (оптимальный баланс между скоростью и степенью сжатия для динамического контента).
