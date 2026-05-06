package pool

import (
	"murl/internal/dto"
	"testing"
)

func TestPoolWithAddURL(t *testing.T) {
	// Создаем пул для указателей на AddURL
	p := New[*dto.AddURL]()

	// 1. Проверяем Get на пустом пуле
	if _, ok := p.Get(); ok {
		t.Fatal("Ожидалось ok=false для пустого пула")
	}

	// 2. Создаем объект, заполняем данными и кладем в пул
	obj := &dto.AddURL{
		OriginalURL:  "https://google.com",
		ShortURL:     "goog",
		ConflictFlag: true,
	}

	p.Put(obj)

	// 3. Проверяем, что Put вызвал Reset
	if obj.OriginalURL != "" || obj.ShortURL != "" || obj.ConflictFlag != false {
		t.Errorf("Объект не был сброшен при Put: %+v", obj)
	}

	// 4. Проверяем получение объекта из пула
	returnedObj, ok := p.Get()
	if !ok {
		t.Fatal("Ожидалось получение объекта из пула")
	}

	if returnedObj != obj {
		t.Error("Полученный объект не совпадает с тем, что был помещен в пул")
	}
}
