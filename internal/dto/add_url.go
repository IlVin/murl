package dto

// AddURL представляет собой объект передачи данных для операции регистрации новой ссылки.
// Используется как при входящих запросах на сокращение, так и в качестве события
// для сохранения в журнале событий (Event Storage).
type AddURL struct {
	// OriginalURL — исходный длинный URL, который необходимо сократить.
	OriginalURL string `json:"original_url"`
	// ShortURL — сгенерированный или полученный в результате сокращения короткий идентификатор/URL.
	ShortURL string `json:"short_url"`
	// ConflictFlag указывает на то, что при сохранении произошел конфликт (например, URL уже существует).
	// Помогает вышестоящим слоям определить HTTP статус ответа (например, 409 Conflict).
	ConflictFlag bool `json:"conflict_flag"`
}

// EventType возвращает тип события EvAddURL.
// Позволяет идентифицировать структуру при десериализации из лога событий.
func (AddURL) EventType() EvType { return EvAddURL }
