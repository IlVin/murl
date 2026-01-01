package handlers

import (
	"errors"
	"murl/internal/config"
	"murl/internal/repository"
	"murl/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"

	resty "github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ErrNoRedirect error = errors.New("no redirect")

type plan struct {
	name  string
	lURL  string
	sCode int
	sURL  string
	err   error
}

func TestAddReq(t *testing.T) {

	for _, rType := range []string{"mux", "chi"} {
		cfg := config.GetConfig()
		drv := repository.NewInMemoryDrv(cfg)
		store := repository.NewStore(cfg, drv)
		service := service.NewService(cfg, store)
		hndlrs := NewHandlers(cfg, service)
		router := NewRouter(cfg.Modify(map[string]string{"RouterType": rType}), hndlrs)
		srv := httptest.NewServer(router)
		defer srv.Close()

		testPlan := []plan{
			{name: "1st Add First lURL", lURL: "https://iv77msk.ru/about", sURL: "http://localhost:8080/AAA", sCode: http.StatusCreated, err: nil},
			{name: "1st Add Second lURL", lURL: "https://iv77msk.ru/", sURL: "http://localhost:8080/AAQ", sCode: http.StatusCreated, err: nil},
			{name: "1st Add Third lURL", lURL: "http://iv77msk.ru/", sURL: "http://localhost:8080/AAg", sCode: http.StatusCreated, err: nil},
			{name: "2nd Add First lURL", lURL: "https://iv77msk.ru/about", sURL: "http://localhost:8080/AAA", sCode: http.StatusCreated, err: nil},
			{name: "2nd Add Second lURL", lURL: "https://iv77msk.ru/", sURL: "http://localhost:8080/AAQ", sCode: http.StatusCreated, err: nil},
			{name: "2nd Add Third lURL", lURL: "http://iv77msk.ru/", sURL: "http://localhost:8080/AAg", sCode: http.StatusCreated, err: nil},
			{name: "Add Add Bad lURL", lURL: "://iv77msk.ru/", sCode: http.StatusBadRequest, err: nil},
			{name: "Add Add Bad lURL", lURL: "ftp://iv77msk.ru/", sCode: http.StatusBadRequest, err: nil},
		}

		for _, p := range testPlan {
			t.Run(p.name, func(t *testing.T) {

				client := resty.New()
				resp, err := client.R().SetBody(
					[]byte(p.lURL),
				).SetHeader(
					"Content-Type", "text/plain",
				).Post(srv.URL)

				require.NoError(t, err)
				assert.Equal(t, p.sCode, resp.StatusCode())
				assert.Equal(t, p.sURL, string(resp.Body()))
			})
		}
	}
}

func TestGetReq(t *testing.T) {
	for _, rType := range []string{"mux", "chi"} {

		cfg := config.GetConfig()
		drv := repository.NewInMemoryDrv(cfg)
		store := repository.NewStore(cfg, drv)
		service := service.NewService(cfg, store)
		hndlrs := NewHandlers(cfg, service)
		router := NewRouter(cfg.Modify(map[string]string{"RouterType": rType}), hndlrs)
		srv := httptest.NewServer(router)
		defer srv.Close()

		testPlan := []plan{
			{name: "1st Get First lURL", lURL: "https://iv77msk.ru/about", sURL: srv.URL + "/AAA", sCode: http.StatusTemporaryRedirect, err: ErrNoRedirect},
			{name: "1st Get Second lURL", lURL: "https://iv77msk.ru/", sURL: srv.URL + "/AAQ", sCode: http.StatusTemporaryRedirect, err: ErrNoRedirect},
			{name: "1st Get Third lURL", lURL: "http://iv77msk.ru/", sURL: srv.URL + "/AAg", sCode: http.StatusTemporaryRedirect, err: ErrNoRedirect},
			{name: "Get 404 lURL", sURL: srv.URL + "/CCCC", sCode: http.StatusBadRequest, err: nil},
			{name: "Get 404 lURL", sURL: srv.URL + "/AAA/", sCode: http.StatusBadRequest, err: nil},
			{name: "Get 404 lURL", sURL: srv.URL, sCode: http.StatusBadRequest, err: nil},
		}

		for _, p := range testPlan {
			if p.err == ErrNoRedirect {
				client := resty.New()
				_, err := client.R().SetBody(
					[]byte(p.lURL),
				).SetHeader(
					"Content-Type", "text/plain",
				).Post(srv.URL)
				assert.NoError(t, err)
			}
		}

		for _, p := range testPlan {
			t.Run(p.name, func(t *testing.T) {
				client := &http.Client{
					CheckRedirect: func(req *http.Request, via []*http.Request) error {
						return ErrNoRedirect
					},
				}
				resp, err := client.Get(p.sURL)
				assert.Equal(t, p.sCode, resp.StatusCode)
				if p.err != nil {
					require.ErrorIs(t, err, ErrNoRedirect)
					assert.Equal(t, p.lURL, resp.Header.Get("Location"))
				} else {
					require.NoError(t, err)
				}
				defer resp.Body.Close()

			})
		}
	}
}
