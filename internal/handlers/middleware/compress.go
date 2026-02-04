package middleware

import (
	"cmp"
	"compress/flate"
	"compress/gzip"
	"io"
	"murl/internal/config"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
	"go.uber.org/zap"
)

type ICompressConfig interface {
	config.IZapLogger
	CompressibleContentTypes() map[string]struct{}
}

type TCodec struct {
	Name string
	io.Writer
	io.Closer
	q float64
}

func NewNonCompressionCodec(w http.ResponseWriter) TCodec {
	return TCodec{
		Name:   "identity",
		Writer: w,
	}
}

func (cw *CompressResponseWriter) getCodec() TCodec {
	codecs := make([]TCodec, 0, 4)

	// Собираем кодеки из Accept-Encoding запроса
	for _, headerValue := range cw.r.Header.Values("Accept-Encoding") {
		for _, rawPart := range strings.Split(headerValue, ",") {
			parts := strings.SplitN(strings.TrimSpace(rawPart), ";q=", 2)
			name := strings.ToLower(strings.TrimSpace(parts[0]))
			if name == "" {
				continue
			}
			q := 1.00
			if len(parts) == 2 {
				parsedWeight, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					q = parsedWeight
				}
			}
			if q > 0.0 {
				codecs = append(codecs, TCodec{
					Name: name,
					q:    q,
				})
			}
		}
	}

	// Сортируем по q
	slices.SortStableFunc(codecs, func(a, b TCodec) int {
		return cmp.Compare(b.q, a.q)
	})

	// Ищем первый поддерживаемый нами кодек
	for _, codec := range codecs {
		switch codec.Name {
		case "br":
			c := cw.pools["br"].Get().(*brotli.Writer)
			c.Reset(cw.w)
			codec.Writer = c
			codec.Closer = c
			return codec
		case "gzip":
			c := cw.pools["gzip"].Get().(*gzip.Writer)
			c.Reset(cw.w)
			codec.Writer = c
			codec.Closer = c
			return codec
		case "deflate":
			c := cw.pools["deflate"].Get().(*flate.Writer)
			c.Reset(cw.w)
			codec.Writer = c
			codec.Closer = c
			return codec
		}
	}

	return NewNonCompressionCodec(cw.w)
}

type CompressResponseWriter struct {
	w                        http.ResponseWriter
	r                        *http.Request
	compressibleContentTypes map[string]struct{}
	headerWritten            bool
	codec                    TCodec
	zap                      *zap.Logger
	pools                    map[string]*sync.Pool
}

func NewCompressResponseWriter(zap *zap.Logger, w http.ResponseWriter, r *http.Request, compressibleContentTypes map[string]struct{}, pools map[string]*sync.Pool) *CompressResponseWriter {
	return &CompressResponseWriter{
		zap:                      zap,
		w:                        w,
		r:                        r,
		compressibleContentTypes: compressibleContentTypes,
		codec:                    NewNonCompressionCodec(w),
		pools:                    pools,
	}
}

func (cw *CompressResponseWriter) Header() http.Header {
	return cw.w.Header()
}

// Metrics - указатель на структуру метрик. По умолчанию nil
func (cw *CompressResponseWriter) Metrics() *TMetrics {
	m, ok := cw.r.Context().Value(ctxMetricsKey).(*TMetrics)
	if !ok {
		return nil
	}
	return m
}

func (cw *CompressResponseWriter) WriteHeader(statusCode int) {
	if cw.headerWritten {
		return
	}

	// Смотрим Content-Type - можно ли его паковать?
	contentType := strings.ToLower(cw.Header().Get("Content-Type"))
	if pos := strings.Index(contentType, ";"); pos > -1 {
		contentType = strings.TrimSpace(contentType[0:pos])
	}
	// Проверяем возможность сжимать отправляемого Content-Type
	if _, ok := cw.compressibleContentTypes[contentType]; ok {
		// Проверяем, а не сжат ли уже контент?
		contentEncoding := strings.ToLower(cw.w.Header().Get("Content-Encoding"))
		if contentEncoding == "" || contentEncoding == "identity" {
			cw.codec = cw.getCodec()
			if cw.codec.Name != "identity" {
				cw.Header().Del("Content-Length")
				cw.w.Header().Set("Content-Encoding", cw.codec.Name)
			}
		}
	}

	// Пишем Header
	cw.Header().Add("Vary", "Accept-Encoding") // По стандартам кэширования, Vary: Accept-Encoding нужно добавлять всегда, если сервер в принципе поддерживает сжатие
	cw.w.WriteHeader(statusCode)
	cw.headerWritten = true

	// Сохраняем метрики
	if m := cw.Metrics(); m != nil {
		m.IsCompressed = cw.codec.Name != "identity"
	}

}

func (cw *CompressResponseWriter) Close() (err error) {
	if cw.codec.Closer == nil {
		return nil
	}

	err = cw.codec.Close()

	// 2. Возвращаем в пул компрессор кодека
	switch cw.codec.Name {
	case "gzip":
		if w, ok := cw.codec.Writer.(*gzip.Writer); ok {
			cw.pools["gzip"].Put(w)
		}
	case "br":
		if w, ok := cw.codec.Writer.(*brotli.Writer); ok {
			cw.pools["br"].Put(w)
		}
	case "deflate":
		if w, ok := cw.codec.Writer.(*flate.Writer); ok {
			cw.pools["deflate"].Put(w)
		}
	}

	cw.codec.Closer = nil

	return err
}

func (cw *CompressResponseWriter) Write(p []byte) (int, error) {
	if !cw.headerWritten {
		cw.WriteHeader(http.StatusOK)
	}

	n, err := cw.codec.Write(p)

	if m := cw.Metrics(); m != nil {
		m.OriginalSize += int64(len(p))
	}

	return n, err
}

type readCloserWrapper struct {
	io.Reader
	closer func() error
}

func (w *readCloserWrapper) Close() error {
	return w.closer()
}

func WithCompress(cfg ICompressConfig, h http.Handler) http.Handler {
	compressibleContentTypes := cfg.CompressibleContentTypes()

	// Пулы для чтения (распаковка запроса)
	rPools := map[string]*sync.Pool{
		"gzip":    {New: func() any { return new(gzip.Reader) }},
		"deflate": {New: func() any { return flate.NewReader(nil) }},
	}

	wPools := map[string]*sync.Pool{
		"br":      {New: func() any { return brotli.NewWriter(nil) }},
		"gzip":    {New: func() any { w, _ := gzip.NewWriterLevel(nil, gzip.DefaultCompression); return w }},
		"deflate": {New: func() any { w, _ := flate.NewWriter(nil, flate.DefaultCompression); return w }},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoding := strings.ToLower(r.Header.Get("Content-Encoding"))

		// Обработка сжатого тела запроса
		if encoding != "" && encoding != "identity" {
			var decompressedBody io.ReadCloser
			oldBody := r.Body

			switch encoding {
			case "gzip":
				zr := rPools["gzip"].Get().(*gzip.Reader)
				if err := zr.Reset(oldBody); err != nil {
					cfg.Zap().Error("gzip reset error", zap.Error(err))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				decompressedBody = &readCloserWrapper{
					Reader: zr,
					closer: func() error {
						if err := zr.Close(); err != nil {
							cfg.Zap().Error("gzip close error", zap.Error(err))
						}
						rPools["gzip"].Put(zr)
						return oldBody.Close()
					},
				}
			case "deflate":
				fr := rPools["deflate"].Get().(flate.Resetter)
				if err := fr.Reset(oldBody, nil); err != nil {
					cfg.Zap().Error("deflate reset error", zap.Error(err))
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				decompressedBody = &readCloserWrapper{
					Reader: fr.(io.Reader),
					closer: func() error {
						rPools["deflate"].Put(fr)
						return oldBody.Close()
					},
				}

			case "br":
				br := brotli.NewReader(oldBody)
				decompressedBody = &readCloserWrapper{
					Reader: br,
					closer: oldBody.Close,
				}
			}

			if decompressedBody != nil {
				r.Body = decompressedBody
			}
		}

		cw := NewCompressResponseWriter(cfg.Zap(), w, r, compressibleContentTypes, wPools)
		defer cw.Close()
		h.ServeHTTP(cw, r)
	})
}
