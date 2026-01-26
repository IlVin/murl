package service

import (
	"murl/internal/config"
	"murl/internal/repository"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddURL(t *testing.T) {
	cfg, err := config.GetConfig(nil, nil)
	assert.Nil(t, err)
	drv := repository.NewInMemoryDrv(cfg)
	store := repository.NewStore(cfg, drv)
	service := NewService(cfg, store)

	testPlan := []struct {
		name string
		lURL string
		sURL string
	}{
		{name: "1st good URL", lURL: "http://iv77msk.ru/about/", sURL: "http://localhost:8080/AAA"},
		{name: "2nd good URL", lURL: "http://iv77msk.ru/jobs/", sURL: "http://localhost:8080/AAQ"},
		{name: "3rd good URL", lURL: "http://yandex.ru/school/", sURL: "http://localhost:8080/AAg"},
		{name: "Second 1st good URL", lURL: "http://iv77msk.ru/about/", sURL: "http://localhost:8080/AAA"},
		{name: "Second 2nd good URL", lURL: "http://iv77msk.ru/jobs/", sURL: "http://localhost:8080/AAQ"},
		{name: "Second 3rd good URL", lURL: "http://yandex.ru/school/", sURL: "http://localhost:8080/AAg"},
	}
	for _, test := range testPlan {
		t.Run(test.name, func(t *testing.T) {
			sURL, err := service.AddURL(test.lURL)
			assert.NoError(t, err)
			assert.Equal(t, test.sURL, sURL)

			lURL, err := service.GetURL(sURL)
			assert.NoError(t, err)
			assert.Equal(t, test.lURL, lURL)
		})
	}
}

func TestAddURLErrors(t *testing.T) {
	cfg, err := config.GetConfig(nil, nil)
	assert.Nil(t, err)
	drv := repository.NewInMemoryDrv(cfg)
	store := repository.NewStore(cfg, drv)
	service := NewService(cfg, store)

	testPlan := []struct {
		name string
		lURL string
		err  error
	}{
		{name: "1st bad URL", lURL: "://bad", err: ErrURLBadFormat},
		{name: "2nd bad URL", lURL: "/bad", err: ErrURLNoAbs},
		{name: "3rd bad URL", lURL: "ftp://localhost:8080/bad", err: ErrURLBadScheme},
		{name: "3rd bad URL", lURL: "ftp://localhost:8080/bad", err: ErrURLBadScheme},
	}
	for _, test := range testPlan {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.AddURL(test.lURL)
			assert.ErrorIs(t, err, test.err)
		})
	}
}

func TestGetURL(t *testing.T) {
	cfg, err := config.GetConfig(nil, nil)
	assert.Nil(t, err)
	drv := repository.NewInMemoryDrv(cfg)
	store := repository.NewStore(cfg, drv)
	service := NewService(cfg, store)

	testPlan := []struct {
		name string
		lURL string
		sURL string
	}{
		{name: "1st good URL", lURL: "http://iv77msk.ru/about/", sURL: "http://localhost:8080/AAA"},
		{name: "2nd good URL", lURL: "http://iv77msk.ru/jobs/", sURL: "http://localhost:8080/AAQ"},
		{name: "3rd good URL", lURL: "http://yandex.ru/school/", sURL: "http://localhost:8080/AAg"},
	}
	for _, test := range testPlan {
		t.Run(test.name, func(t *testing.T) {
			sURL, err := service.AddURL(test.lURL)
			assert.NoError(t, err)
			assert.Equal(t, test.sURL, sURL)

			lURL, err := service.GetURL(sURL)
			assert.NoError(t, err)
			assert.Equal(t, test.lURL, lURL)
		})
	}
}

func TestGetURLErrors(t *testing.T) {
	cfg, err := config.GetConfig(nil, nil)
	assert.Nil(t, err)
	drv := repository.NewInMemoryDrv(cfg)
	store := repository.NewStore(cfg, drv)
	service := NewService(cfg, store)

	testPlan := []struct {
		name string
		sURL string
		err  error
	}{
		{name: "1st bad URL", sURL: "://bad", err: ErrURLBadFormat},
		{name: "2nd bad URL", sURL: "/bad", err: ErrDataNotLoad},
	}
	for _, test := range testPlan {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.GetURL(test.sURL)
			assert.ErrorIs(t, err, test.err)
		})
	}
}
