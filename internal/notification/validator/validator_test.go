package notificationvalidator_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	notificationdto "notification-service/internal/notification/dto"
	notificationvalidator "notification-service/internal/notification/validator"
)

func TestValidateAdminCreate_OK(t *testing.T) {
	v := notificationvalidator.New()
	fields, err := v.ValidateAdminCreate(notificationdto.AdminCreateRequest{
		Title:    "Hello",
		Message:  "World",
		UserIDs:  []string{"11111111-1111-1111-1111-111111111111"},
		Channels: []string{"in_app", "email"},
		Priority: "normal",
	})
	require.NoError(t, err)
	require.Nil(t, fields)
}

func TestValidateAdminCreate_MissingFields(t *testing.T) {
	v := notificationvalidator.New()
	fields, err := v.ValidateAdminCreate(notificationdto.AdminCreateRequest{})
	require.Error(t, err)
	require.Contains(t, fields, "title")
	require.Contains(t, fields, "message")
	require.Contains(t, fields, "user_ids")
	require.Contains(t, fields, "channels")
}

func TestValidateInternalCreate_RequiresIdempotencyKey(t *testing.T) {
	v := notificationvalidator.New()
	fields, err := v.ValidateInternalCreate(notificationdto.InternalCreateRequest{
		UserID:   "11111111-1111-1111-1111-111111111111",
		Title:    "t",
		Message:  "m",
		Channels: []string{"in_app"},
	})
	require.Error(t, err)
	require.Contains(t, fields, "idempotency_key")
}

func TestValidateChannels_Invalid(t *testing.T) {
	v := notificationvalidator.New()
	fields, err := v.ValidateAdminCreate(notificationdto.AdminCreateRequest{
		Title:    "t",
		Message:  "m",
		UserIDs:  []string{"11111111-1111-1111-1111-111111111111"},
		Channels: []string{"fax"},
	})
	require.Error(t, err)
	require.Contains(t, fields, "channels[0]")
}
