package handlers

import (
	"context"
	"errors"
	"log/slog"
	"murl/internal/config"
	"murl/internal/model"
	"murl/internal/service"
	pb "murl/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// MurlServer поддерживает API по GRPC протоколу.
type MurlGRPCServer struct {
	pb.UnimplementedShortenerServiceServer
	service    MicroURLService
	keySession config.KeySession
}

// NewMurlGRPCServer конструктор сервера
func NewMurlGRPCServer(cfg HandlersConfig, service MicroURLService) *MurlGRPCServer {
	return &MurlGRPCServer{
		service:    service,
		keySession: cfg.KeySession(),
	}
}

func ErrHandlingGRPC(err error) error {
	if err == nil {
		return nil
	}

	if _, ok := status.FromError(err); ok {
		return err
	}

	if errors.Is(err, service.ErrInvalidURLFormat) {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if errors.Is(err, service.ErrDomainIsBlocked) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}

	if errors.Is(err, service.ErrQuotaReached) {
		return status.Error(codes.ResourceExhausted, err.Error())
	}

	if errors.Is(err, service.ErrConflict) {
		return status.Error(codes.AlreadyExists, err.Error())
	}

	if errors.Is(err, service.ErrNoContent) {
		return status.Error(codes.NotFound, err.Error())
	}

	if errors.Is(err, service.ErrGone) {
		return status.Error(codes.NotFound, err.Error())
	}

	return status.Error(codes.Internal, "internal server error")
}

func (s *MurlGRPCServer) ShortenURL(ctx context.Context, req *pb.URLShortenRequest) (*pb.URLShortenResponse, error) {
	originalURL := req.GetUrl()
	shortURL, err := s.service.AddURL(ctx, originalURL)
	if err != nil {
		slog.Warn("service cannot add URL",
			slog.String("URL", originalURL),
			slog.Any("err", err),
		)
		return nil, ErrHandlingGRPC(err)
	}

	slog.Info("URL shortened",
		slog.String("original_url", originalURL),
		slog.String("short_url", shortURL),
	)

	var resp = &pb.URLShortenResponse{}
	resp.SetResult(shortURL)

	return resp, nil
}

func (s *MurlGRPCServer) ExpandURL(ctx context.Context, req *pb.URLExpandRequest) (*pb.URLExpandResponse, error) {
	u, err := s.service.GetURL(ctx, req.GetId())
	if err != nil {
		return nil, ErrHandlingGRPC(err)
	}

	resp := &pb.URLExpandResponse{}
	resp.SetResult(u)
	return resp, nil
}

func (s *MurlGRPCServer) ListUserURLs(ctx context.Context, req *emptypb.Empty) (*pb.UserURLsResponse, error) {
	session, ok := model.GetSession(ctx, s.keySession)
	if !ok {
		slog.Warn("APIUserURLs unauthorized request")
		return nil, status.Error(codes.Unauthenticated, "Unauthenticated request")
	}

	URLs, err := s.service.GetURLBySessionID(ctx, session)

	if err != nil {
		slog.Warn("service cannot GetURLBySessionID",
			slog.String("sessionID", session.ID.String()),
			slog.Any("err", err),
		)
		return nil, ErrHandlingGRPC(err)
	}

	result := make([]*pb.URLData, 0, len(URLs.Result))

	for _, item := range URLs.Result {
		rItem := pb.URLData_builder{
			ShortUrl:    &item.ShortURL,
			OriginalUrl: &item.OriginalURL,
		}
		result = append(result, rItem.Build())
	}

	resp := &pb.UserURLsResponse{}
	resp.SetUrl(result)

	return resp, nil
}
