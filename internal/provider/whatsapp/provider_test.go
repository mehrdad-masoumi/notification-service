package whatsappprovider_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	providerrerrors "notification-service/internal/provider"
	whatsappprovider "notification-service/internal/provider/whatsapp"
)

func TestWhatsAppProvider_DisabledIsPermanentError(t *testing.T) {
	p := whatsappprovider.New(config.WhatsApp{Enabled: false})
	_, err := p.Send(context.Background(), notificationcontract.SendRequest{To: "+989120000000"})
	require.Error(t, err)
	require.True(t, providerrerrors.IsPermanent(err))
	require.False(t, providerrerrors.IsTemporary(err))
}

func TestWhatsAppProvider_MissingRecipientIsPermanent(t *testing.T) {
	p := whatsappprovider.New(config.WhatsApp{Enabled: true, Provider: "meta", APIKey: "key"})
	_, err := p.Send(context.Background(), notificationcontract.SendRequest{})
	require.Error(t, err)
	require.True(t, providerrerrors.IsPermanent(err))
}
