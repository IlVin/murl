# Event Module

Пакет для организации событийно-ориентированной архитектуры на Go. Обеспечивает строгую типизацию данных (Payload) внутри универсального контейнера (Event).

### Основные возможности:
- **Интерфейсный доступ**: Работа с событиями через чистый интерфейс `Event`.
- **Lazy Unmarshaling**: Полезная нагрузка не декодируется до момента вызова `GetPayload`, что экономит ресурсы при простой пересылке.
- **Type Safety**: Встроенная проверка на соответствие типов при попытке извлечь данные.
- **Авто-идентификация**: Каждое событие получает уникальный UUID при создании.

## Установка

Для работы модуля требуется пакет `google/uuid`:

```bash
go get github.com/google/uuid
```

## Быстрый старт

### Создание и упаковка события
```go
// 1. Подготовка данных
p := event.PayloadAddURL{
    ShardID: 1,
    ID:      1024,
    URL:     "https://example.com",
}

// 2. Создание события (автоматически назначается ID и тип)
ev, err := event.MakeEvent(p)
if err != nil {
    panic(err)
}

// 3. Сериализация в JSON для передачи (в Kafka, Redis, HTTP)
data, _ := ev.Serialize()
```

### Получение и распаковка

```go
// 1. Восстановление из байтов
newEv, _ := event.Parse(data)

// 2. Распаковка данных в структуру
var result event.PayloadAddURL
if err := newEv.GetPayload(&result); err != nil {
    fmt.Printf("Ошибка или несовпадение типов: %v", err)
}
```

## Добавление новых типов событий

Модуль легко расширяется без изменения основной логики:

1. **Обновите константы типов**:
```go
const (
    EvUnknown EvType = iota
    EvAddURL
    EvUserLogin // Новый тип
)
```
2. **Определите структуру Payload**
```go
type PayloadUserLogin struct {
    UserID string `json:"user_id"`
}

// Реализуйте интерфейс Payload
func (PayloadUserLogin) EventType() EvType { return EvUserLogin }
```

## API Reference

| Метод / Функция | Описание |
| :--- | :--- |
| `MakeEvent(p Payload)` | Создает новое событие с уникальным UUID. |
| `Parse(data []byte)` | Восстанавливает событие из JSON-представления. |
| `GetID()` | Возвращает UUID события. |
| `GetType()` | Возвращает числовой код типа события. |
| `Serialize()` | Маршалинг события в формат для передачи по сети. |
| `GetPayload(dest)` | Десериализует данные в `dest` с проверкой типа. |



