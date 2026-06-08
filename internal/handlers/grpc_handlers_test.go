package handlers

import (
	"context"
	"fmt"
	"log"
	"net"
	"testing"
	"time"

	"murl/internal/config"
	"murl/internal/dto"
	"murl/internal/model"
	"murl/internal/service"
	pb "murl/proto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Настраиваем буфер в памяти для эмуляции сетевого соединения
const bufSize = 1024 * 1024

func TestMurlGRPCServer_Integration(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// 1. Мокаем слой конфигурации
	mockCfg := NewMockHandlersConfig(ctrl)
	keySession := config.KeySession("murl_session")
	mockCfg.EXPECT().KeySession().Return(keySession).AnyTimes()

	// 2. Мокаем бизнес-логику (сервис)
	mockService := NewMockMicroURLService(ctrl)

	// 3. Настраиваем gRPC сервер внутри буфера памяти bufconn
	lis := bufconn.Listen(bufSize)
	grpcServer := grpc.NewServer()

	// Регистрируем наш хендлер
	serverHandler := NewMurlGRPCServer(mockCfg, mockService)
	pb.RegisterShortenerServiceServer(grpcServer, serverHandler)

	// Запускаем сервер в фоновой горутине
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("Server exited with error: %v", err)
		}
	}()
	defer grpcServer.Stop()

	// 4. Настраиваем gRPC клиент, работающий с этим буфером
	ctx := context.Background()
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer conn.Close()

	client := pb.NewShortenerServiceClient(conn)

	// --- ТЕСТОВЫЕ СЦЕНАРИИ ---

	t.Run("ShortenURL - Success", func(t *testing.T) {
		targetURL := "https://google.com"
		expectedShort := "http://localhost:8080/abc"

		// Настраиваем ожидание вызова бизнес-логики
		mockService.EXPECT().
			AddURL(gomock.Any(), targetURL).
			Return(expectedShort, nil)

		// Формируем запрос через Opaque Builder
		req := pb.URLShortenRequest_builder{
			Url: &targetURL,
		}.Build()

		resp, err := client.ShortenURL(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, expectedShort, resp.GetResult())
	})

	t.Run("ShortenURL - Invalid Format Error", func(t *testing.T) {
		badURL := "not-a-valid-url"

		mockService.EXPECT().
			AddURL(gomock.Any(), badURL).
			Return("", service.ErrInvalidURLFormat)

		req := pb.URLShortenRequest_builder{
			Url: &badURL,
		}.Build()

		resp, err := client.ShortenURL(ctx, req)
		assert.Nil(t, resp)

		// Проверяем, что ErrHandlingGRPC правильно транслировал ошибку в статус-код gRPC
		grpcStatus, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, grpcStatus.Code())
	})

	t.Run("ShortenURL - Conflict (URL is Already Shortened)", func(t *testing.T) {
		targetURL := "https://github.com"
		existingShort := "http://localhost:8080/xyz123"

		// Эмулируем поведение сервиса: возвращается ссылка, но с оберткой ErrConflict
		conflictErr := fmt.Errorf("URL already shortened: %w", service.ErrConflict)

		mockService.EXPECT().
			AddURL(gomock.Any(), targetURL).
			Return(existingShort, conflictErr)

		req := pb.URLShortenRequest_builder{
			Url: &targetURL,
		}.Build()

		resp, err := client.ShortenURL(ctx, req)
		assert.Nil(t, resp)
		require.Error(t, err)

		// ErrHandlingGRPC должен смаппить это в AlreadyExists (HTTP 409)
		grpcStatus, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.AlreadyExists, grpcStatus.Code())
	})

	t.Run("ShortenURL - Domain Is Blocked Preventer", func(t *testing.T) {
		loopURL := "http://localhost:8080/recursive"

		mockService.EXPECT().
			AddURL(gomock.Any(), loopURL).
			Return("", service.ErrDomainIsBlocked)

		req := pb.URLShortenRequest_builder{
			Url: &loopURL,
		}.Build()

		resp, err := client.ShortenURL(ctx, req)
		assert.Nil(t, resp)
		require.Error(t, err)

		// Проверяем маппинг в FailedPrecondition (HTTP 422)
		grpcStatus, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.FailedPrecondition, grpcStatus.Code())
	})

	t.Run("ExpandURL - Success", func(t *testing.T) {
		id := "abc"
		expectedOriginal := "https://google.com"

		mockService.EXPECT().
			GetURL(gomock.Any(), id).
			Return(expectedOriginal, nil)

		req := pb.URLExpandRequest_builder{
			Id: &id,
		}.Build()

		resp, err := client.ExpandURL(ctx, req)
		assert.NoError(t, err)
		assert.Equal(t, expectedOriginal, resp.GetResult())
	})

	t.Run("ExpandURL - Resource Is Gone (Deleted Link)", func(t *testing.T) {
		id := "deletedLink"

		mockService.EXPECT().
			GetURL(gomock.Any(), id).
			Return("", service.ErrGone)

		req := pb.URLExpandRequest_builder{
			Id: &id,
		}.Build()

		resp, err := client.ExpandURL(ctx, req)
		assert.Nil(t, resp)
		require.Error(t, err)

		// ErrHandlingGRPC маппит ErrGone в codes.NotFound (HTTP 410 -> gRPC NotFound)
		grpcStatus, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.NotFound, grpcStatus.Code())
	})

	t.Run("ListUserURLs - Unauthorized (No Session)", func(t *testing.T) {
		req := &emptypb.Empty{}

		// Передаем пустой контекст без сессии
		resp, err := serverHandler.ListUserURLs(context.Background(), req)

		assert.Nil(t, resp)
		grpcStatus, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, grpcStatus.Code())
	})

	t.Run("ListUserURLs - Empty Content (ErrNoContent)", func(t *testing.T) {
		mockSession := model.Session{
			ID:  uuid.New(),
			TTL: time.Now().Add(time.Hour),
		}
		authCtx := context.WithValue(context.Background(), keySession, mockSession)

		// Эмулируем поведение сервиса, когда у гостя еще нет сохраненных URL
		mockService.EXPECT().
			GetURLBySessionID(gomock.Any(), mockSession).
			Return(dto.GetURLBySessionID{}, service.ErrNoContent)

		resp, err := serverHandler.ListUserURLs(authCtx, &emptypb.Empty{})
		assert.Nil(t, resp)
		require.Error(t, err)

		// ErrHandlingGRPC транслирует ErrNoContent в codes.NotFound
		grpcStatus, ok := status.FromError(err)
		assert.True(t, ok)
		assert.Equal(t, codes.NotFound, grpcStatus.Code())
	})

	t.Run("ListUserURLs - Success", func(t *testing.T) {
		// Создаем валидную сессию
		sessionID := uuid.New()
		mockSession := model.Session{
			ID:  sessionID,
			TTL: time.Now().Add(time.Hour),
		}

		// Помещаем её в контекст, используя ключ из конфигурации
		authCtx := context.WithValue(context.Background(), keySession, mockSession)

		// Ответ прикладного слоя в формате структуры dto.GetURLBySessionID из service.go
		mockServiceResponse := dto.GetURLBySessionID{
			SessionID: mockSession.ID,
			Result: []dto.URLItem{
				{ShortURL: "http://loc/1", OriginalURL: "https://ya.ru"},
				{ShortURL: "http://loc/2", OriginalURL: "https://go.com"},
			},
		}

		mockService.EXPECT().
			GetURLBySessionID(gomock.Any(), mockSession).
			Return(mockServiceResponse, nil)

		req := &emptypb.Empty{}

		// Вызываем метод, передавая авторизованный контекст
		resp, err := serverHandler.ListUserURLs(authCtx, req)

		assert.NoError(t, err)
		require.NotNil(t, resp)

		// Проверяем, что слайс корректно собрался билдерами
		urls := resp.GetUrl()
		require.Len(t, urls, 2)
		assert.Equal(t, "http://loc/1", urls[0].GetShortUrl())
		assert.Equal(t, "https://ya.ru", urls[0].GetOriginalUrl())
		assert.Equal(t, "http://loc/2", urls[1].GetShortUrl())
		assert.Equal(t, "https://go.com", urls[1].GetOriginalUrl())
	})
}
