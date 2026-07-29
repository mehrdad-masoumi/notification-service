package application_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	application "notification-service/internal/application/notification"
)

func TestSendNotificationCommandChannels(t *testing.T) {
	cmd := application.SendNotificationCommand{
		IdempotencyKey: "k",
		TemplateCode:   "withdrawal_approved",
		SourceService:  "withdrawal-service",
		Recipient: application.Recipient{
			UserID: "11111111-1111-1111-1111-111111111111",
			Email:  "u@example.com",
		},
		Channels: []application.Channel{
			application.ChannelInApp,
			application.ChannelEmail,
		},
	}
	require.Equal(t, "withdrawal_approved", cmd.TemplateCode)
	require.Len(t, cmd.Channels, 2)
}
