# Module: Prometheus Metrics (pgc/metrics)

Реализация системы мониторинга для PostgreSQL на базе Prometheus. Позволяет отслеживать доступность инстансов, производительность транзакций и интенсивность повторных попыток (retries).

---

## Метрики и метки


| Метрика | Тип | Labels | Описание |
| :--- | :--- | :--- | :--- |
| **pg_instance_ready** | Gauge | `instance` | 1 — Online, 0 — Offline. |
| **pg_instance_offline_total** | Counter | `instance` | Общее количество переходов в режим Offline. |
| **pg_instance_retries_total** | Counter | `instance`, `type` | Количество повторных попыток (pool или tx). |
| **pg_instance_duration_seconds**| Histogram | `instance`, `type` | Время выполнения операций (бакеты от 1мс до 5с). |

---

## Использование

Инициализация требует передачи реестра Prometheus (например, `prometheus.DefaultRegisterer`).

```go
reg := prometheus.NewRegistry()
pgMetrics, err := metrics.NewPgMetrics(reg)
if err != nil {
    // Обработка ошибки инициализации
}

// Передача в PgInstance
pg, _ := instance.NewPgInstance(ctx, connStr, pgMetrics)
```

---

## Особенности реализации

### 1. Гистограммы латентности
Используются оптимизированные бакеты для БД:
```go
dbBuckets := []float64{.001, .002, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}
```
Это позволяет точно измерять как быстрые SELECT-запросы (p95 < 10ms), так и тяжелые транзакции.

### 2. Идемпотентность
Метод `NewPgMetrics` корректно обрабатывает ошибку `AlreadyRegisteredError`. Это позволяет безопасно инициализировать метрики в тестах или при горячей перезагрузке конфигурации без падения приложения.

### 3. Маркировка операций
Метка `type` в метриках латентности и ретраев принимает значения:
*   `pool` — при использовании `pg.PgPool`.
*   `tx` — при использовании транзакционного блока `pg.Tx`.
