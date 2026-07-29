package worker

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"notification-service/internal/notification/entity"
	providerrerrors "notification-service/internal/provider"
)

func TestResolveRecipient_UsesStoredContacts(t *testing.T) {
	p := &Processor{}
	email := "u@example.com"
	phone := "+49123456789"
	n := entity.Notification{
		UserID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Email:  &email,
		Phone:  &phone,
	}

	to, err := p.resolveRecipient(n, entity.ChannelInApp)
	require.NoError(t, err)
	require.Equal(t, n.UserID.String(), to)

	to, err = p.resolveRecipient(n, entity.ChannelEmail)
	require.NoError(t, err)
	require.Equal(t, email, to)

	to, err = p.resolveRecipient(n, entity.ChannelSMS)
	require.NoError(t, err)
	require.Equal(t, phone, to)
}

func TestResolveRecipient_MissingContactIsPermanent(t *testing.T) {
	p := &Processor{}
	n := entity.Notification{UserID: uuid.New()}

	_, err := p.resolveRecipient(n, entity.ChannelEmail)
	require.True(t, providerrerrors.IsPermanent(err))

	_, err = p.resolveRecipient(n, entity.ChannelWhatsApp)
	require.True(t, providerrerrors.IsPermanent(err))
}
