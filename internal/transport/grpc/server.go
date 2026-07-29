package grpctransport

import (
	"context"
	"errors"
	"log"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationv1 "github.com/mehrdad-masoumi/broker-contract/gen/go/notification/v1"
	notificationcontract "github.com/mehrdad-masoumi/broker-contract/go"
	"github.com/mehrdad-masoumi/go-packages/apperr"

	application "notification-service/internal/application/notification"
)

// Server adapts gRPC SendNotification to the shared CommandService.
type Server struct {
	notificationv1.UnimplementedNotificationServiceServer
	cmds *application.CommandService
}

func NewServer(cmds *application.CommandService) *Server {
	return &Server{cmds: cmds}
}

func (s *Server) SendNotification(ctx context.Context, req *notificationv1.SendNotificationRequest) (*notificationv1.SendNotificationResponse, error) {
	if req == nil || req.GetNotification() == nil {
		return errorResponse(codes.InvalidArgument, "invalid_argument", "notification is required"), nil
	}

	cmdJSON, err := notificationcontract.FromProto(req.GetNotification())
	if err != nil {
		return errorResponse(codes.InvalidArgument, "invalid_argument", err.Error()), nil
	}
	if err := notificationcontract.ValidateNotificationRequested(cmdJSON); err != nil {
		return errorResponse(codes.InvalidArgument, "validation_failed", err.Error()), nil
	}

	cmd := mapContract(cmdJSON)
	result, err := s.cmds.Send(ctx, cmd)
	if err != nil {
		return mapAppError(err), nil
	}

	st := notificationv1.NotificationStatus_NOTIFICATION_STATUS_ACCEPTED
	switch result.Status {
	case "scheduled":
		st = notificationv1.NotificationStatus_NOTIFICATION_STATUS_SCHEDULED
	case "duplicate":
		st = notificationv1.NotificationStatus_NOTIFICATION_STATUS_DUPLICATE
	}
	if result.Duplicate {
		st = notificationv1.NotificationStatus_NOTIFICATION_STATUS_DUPLICATE
	}

	return &notificationv1.SendNotificationResponse{
		NotificationId: result.NotificationID,
		Status:         st,
		Duplicate:      result.Duplicate,
		AcceptedAt:     timestamppb.New(result.AcceptedAt),
	}, nil
}

func mapContract(n notificationcontract.NotificationRequested) application.SendNotificationCommand {
	channels := make([]application.Channel, 0, len(n.Channels))
	for _, ch := range n.Channels {
		channels = append(channels, application.Channel(ch))
	}
	return application.SendNotificationCommand{
		MessageID:      n.MessageID,
		IdempotencyKey: n.IdempotencyKey,
		SourceService:  n.SourceService,
		TemplateCode:   n.TemplateCode,
		Locale:         n.Locale,
		Recipient: application.Recipient{
			UserID:       n.Recipient.UserID,
			Email:        n.Recipient.Email,
			Phone:        n.Recipient.Phone,
			DeviceTokens: append([]string(nil), n.Recipient.DeviceTokens...),
			DisplayName:  n.Recipient.DisplayName,
		},
		Channels:      channels,
		Variables:     n.Variables,
		Metadata:      n.Metadata,
		ScheduledAt:   n.ScheduledAt,
		CorrelationID: n.CorrelationID,
		TraceID:       n.TraceID,
	}
}

func mapAppError(err error) *notificationv1.SendNotificationResponse {
	var ve *apperr.Error
	if errors.As(err, &ve) && ve != nil && len(ve.Fields) > 0 {
		return errorResponse(codes.InvalidArgument, "validation_failed", ve.Error())
	}
	var re *apperr.RichError
	if errors.As(err, &re) && re != nil {
		switch re.Kind() {
		case apperr.KindInvalid:
			return errorResponse(codes.InvalidArgument, "invalid_argument", re.Error())
		case apperr.KindNotFound:
			return errorResponse(codes.NotFound, "not_found", re.Error())
		case apperr.KindTooManyRequests:
			return errorResponse(codes.ResourceExhausted, "rate_limited", re.Error())
		}
	}
	log.Printf("grpc SendNotification unexpected error type=%T", err)
	return errorResponse(codes.Internal, "internal", "internal error")
}

func errorResponse(_ codes.Code, code, msg string) *notificationv1.SendNotificationResponse {
	return &notificationv1.SendNotificationResponse{
		Status:       notificationv1.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED,
		ErrorCode:    code,
		ErrorMessage: msg,
		AcceptedAt:   timestamppb.Now(),
	}
}

// Runner hosts the gRPC server with health + optional reflection.
type Runner struct {
	addr       string
	dev        bool
	server     *Server
	grpcServer *grpc.Server
}

func NewRunner(addr string, dev bool, svc *Server) *Runner {
	return &Runner{addr: addr, dev: dev, server: svc}
}

func (r *Runner) Start(ctx context.Context) error {
	lis, err := net.Listen("tcp", r.addr)
	if err != nil {
		return err
	}
	r.grpcServer = grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(r.grpcServer, r.server)

	hs := health.NewServer()
	healthpb.RegisterHealthServer(r.grpcServer, hs)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	hs.SetServingStatus(notificationv1.NotificationService_ServiceDesc.ServiceName, healthpb.HealthCheckResponse_SERVING)

	if r.dev {
		reflection.Register(r.grpcServer)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("notification-grpc listening on %s (reflection=%v)", r.addr, r.dev)
		errCh <- r.grpcServer.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func (r *Runner) Stop(ctx context.Context) error {
	if r.grpcServer == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		r.grpcServer.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		r.grpcServer.Stop()
		return ctx.Err()
	case <-time.After(10 * time.Second):
		r.grpcServer.Stop()
		return status.Error(codes.DeadlineExceeded, "grpc graceful stop timeout")
	}
}

// IsDevEnv reports whether reflection should be enabled.
func IsDevEnv(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	return env == "" || env == "development" || env == "dev" || env == "local"
}
