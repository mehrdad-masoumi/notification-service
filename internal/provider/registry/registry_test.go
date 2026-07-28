package providerregistry_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"notification-service/config"
	providerregistry "notification-service/internal/provider/registry"
)

func TestRegistry_AlwaysRegistersInAppAndEmail(t *testing.T) {
	r := providerregistry.New(config.Config{})
	_, err := r.Get("in_app")
	require.NoError(t, err)
	_, err = r.Get("email")
	require.NoError(t, err)
}

func TestRegistry_DoesNotRegisterUnreadyChannels(t *testing.T) {
	r := providerregistry.New(config.Config{})
	_, err := r.Get("sms")
	require.Error(t, err)
	_, err = r.Get("whatsapp")
	require.Error(t, err)
	_, err = r.Get("push")
	require.Error(t, err)
}

func TestRegistry_RegistersReadyChannels(t *testing.T) {
	cfg := config.Config{
		SMS:      config.SMS{Enabled: true, Provider: "twilio", APIKey: "key"},
		WhatsApp: config.WhatsApp{Enabled: true, Provider: "meta", APIKey: "key"},
		Push:     config.Push{Enabled: true, Provider: "fcm"},
	}
	r := providerregistry.New(cfg)
	_, err := r.Get("sms")
	require.NoError(t, err)
	_, err = r.Get("whatsapp")
	require.NoError(t, err)
	_, err = r.Get("push")
	require.NoError(t, err)
}
