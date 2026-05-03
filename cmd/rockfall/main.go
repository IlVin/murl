// Package main предоставляет инструменты для проведения нагрузочного тестирования
// сервиса сокращения ссылок. Поддерживает одиночные и пакетные операции,
// проверку авторизации и асинхронное удаление.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	baseURL     = "http://localhost:8080"
	concurrency = 100
	duration    = 3 * time.Minute
)

// BatchRequest описывает структуру элемента в запросе на пакетное сокращение ссылок.
type BatchRequest struct {
	CorrelationID string `json:"correlation_id"`
	OriginalURL   string `json:"original_url"`
}

// BatchResponse описывает структуру элемента в ответе на пакетное создание ссылок.
type BatchResponse struct {
	CorrelationID string `json:"correlation_id"`
	ShortURL      string `json:"short_url"`
}

// UserURL содержит информацию о паре ссылок, принадлежащих конкретному пользователю.
type UserURL struct {
	ShortURL    string `json:"short_url"`
	OriginalURL string `json:"original_url"`
}

// main инициализирует воркеры и запускает цикл нагрузочного тестирования.
// В процессе работы собирается статистика по количеству операций, ошибок и задержкам.
func main() {
	fmt.Printf("Запуск комплексной нагрузки: %d горутин, время: %s\n", concurrency, duration)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	var (
		written  int64
		verified int64
		errors   int64
		latency  int64 // в микросекундах
	)

	wg := sync.WaitGroup{}
	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// У каждой горутины свой Jar для авторизационных кук
			jar, _ := cookiejar.New(nil)
			client := &http.Client{
				Jar: jar,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
				Timeout: 5 * time.Second,
			}

			for {
				select {
				case <-ctx.Done():
					return
				case <-sigChan:
					cancel()
					return
				default:
					iterStart := time.Now()

					// 1. Одиночный POST (как в оригинале)
					origSingle := fmt.Sprintf("https://example.com/%d/%d", id, time.Now().UnixNano())
					shortSingle, err := postURL(client, origSingle)
					if err != nil {
						atomic.AddInt64(&errors, 1)
						continue
					}
					atomic.AddInt64(&written, 1)

					// 2. Одиночная проверка (как в оригинале)
					err = verifyURL(client, shortSingle, origSingle)
					if err != nil {
						atomic.AddInt64(&errors, 1)
						continue
					}
					atomic.AddInt64(&verified, 1)

					// 3. Пакетный POST (Batch)
					batchCount := 5
					batchShorts, err := postBatch(client, batchCount)
					if err != nil {
						atomic.AddInt64(&errors, 1)
					} else {
						atomic.AddInt64(&written, int64(len(batchShorts)))
					}

					// 4. Проверка своих URL и асинхронное удаление (раз в 10 итераций)
					if time.Now().UnixNano()%10 == 0 {
						userUrls, err := getUserURLs(client)
						if err == nil && len(userUrls) > 0 {
							ids := make([]string, 0)
							for _, u := range userUrls {
								parts := strings.Split(strings.TrimRight(u.ShortURL, "/"), "/")
								ids = append(ids, parts[len(parts)-1])
							}
							_ = deleteURLs(client, ids)
						}
					}

					atomic.AddInt64(&latency, time.Since(iterStart).Microseconds())
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Итоговые расчеты
	totalOps := atomic.LoadInt64(&written) + atomic.LoadInt64(&verified)
	rps := float64(totalOps) / elapsed.Seconds()
	avgLat := time.Duration(0)
	if totalOps > 0 {
		// Делим на 2, т.к. в итерации теперь больше действий, но для совместимости с твоим логом
		avgLat = time.Duration(atomic.LoadInt64(&latency)/totalOps) * time.Microsecond
	}

	fmt.Println("\n" + "━" + sampleScale(40))
	fmt.Printf("Тест завершен за: %s\n", elapsed.Round(time.Second))
	fmt.Printf("Всего записей (single+batch): %d\n", atomic.LoadInt64(&written))
	fmt.Printf("Проверено URL:                %d\n", atomic.LoadInt64(&verified))
	fmt.Printf("Ошибок:                       %d\n", atomic.LoadInt64(&errors))
	fmt.Printf("RPS:                          %.2f оп/сек\n", rps)
	fmt.Printf("Avg Latency:                  %s\n", avgLat)
	fmt.Println("━" + sampleScale(40))
}

// --- Реализация методов ---

// postURL выполняет POST-запрос для сокращения одной ссылки.
// Возвращает сокращенный URL или ошибку, если статус ответа не 201 или 409.
func postURL(client *http.Client, longURL string) (string, error) {
	resp, err := client.Post(baseURL+"/", "text/plain", bytes.NewBufferString(longURL))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		return "", fmt.Errorf("post status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// verifyURL проверяет работоспособность сокращенной ссылки.
// Ожидает редирект (307) на оригинальный адрес. Учитывает возможность удаления ссылки (410).
func verifyURL(client *http.Client, shortURL, originalURL string) error {
	resp, err := client.Get(shortURL)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// 410 Gone - норма для удаленных асинхронно ссылок
	if resp.StatusCode == http.StatusGone {
		return nil
	}

	if resp.StatusCode != http.StatusTemporaryRedirect {
		return fmt.Errorf("get status: %d", resp.StatusCode)
	}

	if resp.Header.Get("Location") != originalURL {
		return fmt.Errorf("location mismatch")
	}

	return nil
}

// postBatch отправляет запрос на генерацию сразу нескольких сокращенных ссылок.
// Возвращает список созданных коротких ссылок.
func postBatch(client *http.Client, count int) ([]string, error) {
	batch := make([]BatchRequest, count)
	for i := 0; i < count; i++ {
		batch[i] = BatchRequest{
			CorrelationID: fmt.Sprintf("c_%d", time.Now().UnixNano()),
			OriginalURL:   fmt.Sprintf("https://example.com/%d", time.Now().UnixNano()),
		}
	}

	body, _ := json.Marshal(batch)
	resp, err := client.Post(baseURL+"/api/shorten/batch", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("batch status: %d", resp.StatusCode)
	}

	var res []BatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	urls := make([]string, len(res))
	for i, r := range res {
		urls[i] = r.ShortURL
	}
	return urls, nil
}

// getUserURLs запрашивает список всех ссылок, созданных текущим пользователем (на основе Cookie).
func getUserURLs(client *http.Client) ([]UserURL, error) {
	resp, err := client.Get(baseURL + "/api/user/urls")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user urls status: %d", resp.StatusCode)
	}

	var urls []UserURL
	if err := json.NewDecoder(resp.Body).Decode(&urls); err != nil {
		return nil, err
	}
	return urls, nil
}

// deleteURLs отправляет запрос на пакетное удаление ссылок по их идентификаторам.
func deleteURLs(client *http.Client, ids []string) error {
	body, _ := json.Marshal(ids)
	req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/user/urls", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// sampleScale возвращает строку-разделитель заданной длины для оформления вывода в консоль.
func sampleScale(n int) string {
	return strings.Repeat("-", n)
}
