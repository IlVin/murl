package dto

//go:generate go run murl/cmd/event $GOFILE

// GetURL представляет собой объект передачи данных, используемый при запросе
// оригинального URL по его сокращенному идентификатору.
// Используется в логике редиректов и для фиксации событий перехода.
//
//go:event
type GetURL struct {
	// OriginalURL — исходный длинный URL, на который должен быть выполнен переход.
	OriginalURL string `json:"original_url"`
	// ShortURL — сокращенный идентификатор или полный короткий URL, по которому пришел запрос.
	ShortURL string `json:"short_url"`
	// IsGone указывает на то, что ссылка была удалена (HTTP 410 Gone).
	// Если true, оригинальный URL может быть пуст или не должен использоваться.
	IsGone bool `json:"is_gone,omitempty"`
}
