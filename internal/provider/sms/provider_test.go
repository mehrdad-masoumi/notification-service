package smsprovider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	providerrerrors "notification-service/internal/provider"
	smsprovider "notification-service/internal/provider/sms"
)

func TestSMSProvider_DisabledIsPermanentError(t *testing.T) {
	p := smsprovider.New(config.SMS{Enabled: false})
	_, err := p.Send(context.Background(), notificationcontract.SendRequest{To: "+989120000000"})
	require.Error(t, err)
	require.True(t, providerrerrors.IsPermanent(err))
	require.False(t, providerrerrors.IsTemporary(err))
}

func TestSMSProvider_MissingRecipientIsPermanent(t *testing.T) {
	p := smsprovider.New(config.SMS{Enabled: true, Provider: "twilio", APIKey: "key"})
	_, err := p.Send(context.Background(), notificationcontract.SendRequest{})
	require.Error(t, err)
	require.True(t, providerrerrors.IsPermanent(err))
}

func TestSMSProvider_ReadyButUnimplementedIsPermanent(t *testing.T) {
	p := smsprovider.New(config.SMS{Enabled: true, Provider: "twilio", APIKey: "key"})
	_, err := p.Send(context.Background(), notificationcontract.SendRequest{To: "+989120000000"})
	require.Error(t, err)
	require.True(t, providerrerrors.IsPermanent(err))
}
