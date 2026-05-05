package dto

//go:generate go run murl/cmd/event $GOFILE

// Batch представляет собой контейнер для выполнения пакетных операций сокращения ссылок.
// Используется в сценариях массовой обработки данных без привязки к конкретной сессии пользователя.
//
//generate:event
//generate:reset
type Batch struct {
	// Batch — список элементов (ссылок), подлежащих обработке.
	Batch []BatchItem `json:"batch"`
}
