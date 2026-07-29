package notificationservice

import (
	"testing"

	"github.com/stretchr/testify/require"

	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
)

func TestFilterChannelsByContacts(t *testing.T) {
	channels := []entity.Channel{
		entity.ChannelInApp,
		entity.ChannelEmail,
		entity.ChannelSMS,
		entity.ChannelWhatsApp,
		entity.ChannelPush,
	}

	t.Run("keeps in_app and push always", func(t *testing.T) {
		got := filterChannelsByContacts(channels, notificationdto.Contacts{})
		require.Equal(t, []entity.Channel{entity.ChannelInApp, entity.ChannelPush}, got)
	})

	t.Run("drops unverified email and phone", func(t *testing.T) {
		got := filterChannelsByContacts(channels, notificationdto.Contacts{
			Email: "u@example.com",
			Phone: "+49123456789",
		})
		require.Equal(t, []entity.Channel{entity.ChannelInApp, entity.ChannelPush}, got)
	})

	t.Run("keeps verified contacts", func(t *testing.T) {
		got := filterChannelsByContacts(channels, notificationdto.Contacts{
			Email:         "u@example.com",
			Phone:         "+49123456789",
			EmailVerified: true,
			PhoneVerified: true,
		})
		require.Equal(t, channels, got)
	})

	t.Run("honors preference opt-out", func(t *testing.T) {
		got := filterChannelsByContacts(channels, notificationdto.Contacts{
			Email:         "u@example.com",
			Phone:         "+49123456789",
			EmailVerified: true,
			PhoneVerified: true,
			Preferences:   map[string]bool{"email": false, "sms": false},
		})
		require.Equal(t, []entity.Channel{
			entity.ChannelInApp,
			entity.ChannelWhatsApp,
			entity.ChannelPush,
		}, got)
	})
}
