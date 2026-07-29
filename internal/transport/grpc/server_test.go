package grpctransport_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationv1 "github.com/mehrdad-masoumi/broker-contract/gen/go/notification/v1"

	application "notification-service/internal/application/notification"
	grpctransport "notification-service/internal/transport/grpc"
)

// stubCmds is not used with real DB; we only verify request mapping rejects nil.
func TestSendNotification_NilRequest(t *testing.T) {
	svc := grpctransport.NewServer(application.NewCommandService(nil))
	resp, err := svc.SendNotification(context.Background(), nil)
	require.NoError(t, err)
	require.Equal(t, "invalid_argument", resp.ErrorCode)
}

func TestSendNotification_ValidationFields(t *testing.T) {
	svc := grpctransport.NewServer(application.NewCommandService(nil))
	now := time.Now().UTC()
	resp, err := svc.SendNotification(context.Background(), &notificationv1.SendNotificationRequest{
		Notification: &notificationv1.NotificationRequested{
			Version:       "v1",
			MessageId:     "m1",
			SourceService: "svc",
			TemplateCode:  "t",
			// missing idempotency_key
			Recipient:   &notificationv1.Recipient{Email: "a@b.c"},
			Channels:    []notificationv1.Channel{notificationv1.Channel_CHANNEL_EMAIL},
			RequestedAt: timestamppb.New(now),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "validation_failed", resp.ErrorCode)
}
