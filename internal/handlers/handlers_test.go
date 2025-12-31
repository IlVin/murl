package handlers

import (
	"io"
	"murl/internal/config"
	"murl/internal/repository"
	"murl/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resp struct {
	code        int
	contentType string
	location    string
	body        string
}

type req struct {
	method      string
	path        string
	contentType string
	body        io.Reader
}

func TestHndlAddURL(t *testing.T) {

	cfg := config.GetConfig()
	drv := repository.NewInMemoryDrv(cfg)
	store := repository.NewStore(cfg, drv)
	service := service.NewService(cfg, store)

	testPlan := []struct {
		name     string
		request  req
		response resp
	}{
		{
			name:     "Add URL #1",
			request:  req{method: "POST", path: "/", contentType: "text/plain", body: strings.NewReader("http://iv77msk.ru/about")},
			response: resp{code: http.StatusCreated, contentType: "text/plain", body: "http://localhost:8080/AAA"},
		},
		{
			name:     "Add URL #2",
			request:  req{method: "POST", path: "/", contentType: "text/plain", body: strings.NewReader("https://practicum.yandex.ru/learn/go-advanced/courses/")},
			response: resp{code: http.StatusCreated, contentType: "text/plain", body: "http://localhost:8080/AAQ"},
		},
		{
			name:     "2nd Add URL #1",
			request:  req{method: "POST", path: "/", contentType: "text/plain", body: strings.NewReader("http://iv77msk.ru/about")},
			response: resp{code: http.StatusCreated, contentType: "text/plain", body: "http://localhost:8080/AAA"},
		},
		{
			name:     "2nd Add URL #2",
			request:  req{method: "POST", path: "/", contentType: "text/plain", body: strings.NewReader("https://practicum.yandex.ru/learn/go-advanced/courses/")},
			response: resp{code: http.StatusCreated, contentType: "text/plain", body: "http://localhost:8080/AAQ"},
		},
		{
			name:     "Add No URL",
			request:  req{method: "POST", path: "/", contentType: "text/plain", body: strings.NewReader("")},
			response: resp{code: http.StatusBadRequest, contentType: "", body: ""},
		},
		{
			name:     "Add Bad URL",
			request:  req{method: "POST", path: "/", contentType: "text/plain", body: strings.NewReader("://123")},
			response: resp{code: http.StatusBadRequest, contentType: "", body: ""},
		},
		{
			name:     "Add Wrong URL Schema",
			request:  req{method: "POST", path: "/", contentType: "text/plain", body: strings.NewReader("ftp://123")},
			response: resp{code: http.StatusBadRequest, contentType: "", body: ""},
		},
	}

	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			r := httptest.NewRequest(p.request.method, p.request.path, p.request.body)
			w := httptest.NewRecorder()
			NewHandlers(cfg, service).HndlAddURL()(w, r)
			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, p.response.code, res.StatusCode)
			assert.Equal(t, p.response.contentType, res.Header.Get("Content-Type"))

			resBody, err := io.ReadAll(res.Body)
			require.NoError(t, err)
			assert.Equal(t, p.response.body, string(resBody))
		})
	}
}

func TestHndlGetURLError(t *testing.T) {

	cfg := config.GetConfig()
	drv := repository.NewInMemoryDrv(cfg)
	store := repository.NewStore(cfg, drv)
	service := service.NewService(cfg, store)

	testPlan := []struct {
		name     string
		request  req
		response resp
	}{
		{
			name:     "Get Bad URL Path",
			request:  req{method: "GET", path: "/", contentType: "text/plain", body: nil},
			response: resp{code: http.StatusBadRequest, contentType: "", body: ""},
		},
		{
			name:     "Get Bad URL ID in Path #1",
			request:  req{method: "GET", path: "/BBBB", contentType: "text/plain", body: nil},
			response: resp{code: http.StatusBadRequest, contentType: "", body: ""},
		},
		{
			name:     "Get Bad URL ID in Path #2",
			request:  req{method: "GET", path: "/~BBBB", contentType: "text/plain", body: nil},
			response: resp{code: http.StatusBadRequest, contentType: "", body: ""},
		},
		{
			name:     "Get Bad URL ID in Path #3",
			request:  req{method: "GET", path: "/BB~BB", contentType: "text/plain", body: nil},
			response: resp{code: http.StatusBadRequest, contentType: "", body: ""},
		},
	}

	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			r := httptest.NewRequest(p.request.method, p.request.path, p.request.body)
			w := httptest.NewRecorder()
			NewHandlers(cfg, service).HndlAddURL()(w, r)
			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, p.response.code, res.StatusCode)
			assert.Equal(t, p.response.contentType, res.Header.Get("Content-Type"))

			resBody, err := io.ReadAll(res.Body)
			require.NoError(t, err)
			assert.Equal(t, p.response.body, string(resBody))
		})
	}
}

func TestHndlGetURL(t *testing.T) {

	cfg := config.GetConfig()
	drv := repository.NewInMemoryDrv(cfg)
	store := repository.NewStore(cfg, drv)
	service := service.NewService(cfg, store)

	testPlan := []struct {
		name     string
		request  req
		response resp
	}{
		{
			name:     "Get URL #1",
			request:  req{method: "GET", path: "/AAA", contentType: "text/plain", body: nil},
			response: resp{code: http.StatusTemporaryRedirect, location: "http://iv77msk.ru/about"},
		},
		{
			name:     "Get URL #2",
			request:  req{method: "GET", path: "/AAQ", contentType: "text/plain", body: nil},
			response: resp{code: http.StatusTemporaryRedirect, location: "https://practicum.yandex.ru/learn/go-advanced/courses/"},
		},
	}
	// Add new records to DB
	for _, p := range testPlan {
		NewHandlers(cfg, service).HndlAddURL()(httptest.NewRecorder(), httptest.NewRequest("POST", "/", strings.NewReader(p.response.location)))
	}

	// Testing
	for _, p := range testPlan {
		t.Run(p.name, func(t *testing.T) {
			r := httptest.NewRequest(p.request.method, p.request.path, p.request.body)
			w := httptest.NewRecorder()
			NewHandlers(cfg, service).HndlGetURL()(w, r)
			res := w.Result()
			defer res.Body.Close()

			assert.Equal(t, p.response.code, res.StatusCode)
			assert.Equal(t, p.response.location, res.Header.Get("Location"))
		})
	}
}
