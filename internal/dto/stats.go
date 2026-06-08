package dto

type Stats struct {
	URLs  uint64 `json:"urls,omitempty"`  // количество сокращённых URL в сервисе
	Users uint64 `json:"users,omitempty"` // количество пользователей в сервисе
}
