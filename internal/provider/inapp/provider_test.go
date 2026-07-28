package inappprovider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	notificationcontract "notification-service/internal/notification/contract"
	inappprovider "notification-service/internal/provider/inapp"
)

func TestInAppProvider_Send(t *testing.T) {
	p := inappprovider.New()
	require.Equal(t, "in_app", p.Channel())
	res, err := p.Send(context.Background(), notificationcontract.SendRequest{
		DeliveryID: "d1",
		Title:      "t",
		Message:    "m",
	})
	require.NoError(t, err)
	require.Equal(t, "postgres", res.Provider)
	require.NotEmpty(t, res.MessageID)
}
