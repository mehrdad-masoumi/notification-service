package notificationvalidator_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	notificationdto "notification-service/internal/notification/dto"
	notificationvalidator "notification-service/internal/notification/validator"
)

var allEnabled = map[string]bool{
	"in_app":   true,
	"email":    true,
	"sms":      true,
	"whatsapp": true,
	"push":     true,
}

func TestValidateCommand_TemplateCodeRequired(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateCommand(notificationdto.CommandRequest{
		IdempotencyKey: "key-1",
		UserID:         "11111111-1111-1111-1111-111111111111",
		Contacts:       &notificationdto.Contacts{},
	})
	require.Error(t, err)
	require.Contains(t, fields, "template_code")
}

func TestValidateCommand_ContactsRequired(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateCommand(notificationdto.CommandRequest{
		IdempotencyKey: "key-1",
		UserID:         "11111111-1111-1111-1111-111111111111",
		TemplateCode:   "withdrawal_approved",
	})
	require.Error(t, err)
	require.Contains(t, fields, "contacts")
}

func TestValidateCommand_ChannelsOptional(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateCommand(notificationdto.CommandRequest{
		IdempotencyKey: "key-1",
		UserID:         "11111111-1111-1111-1111-111111111111",
		TemplateCode:   "withdrawal_approved",
		Contacts:       &notificationdto.Contacts{},
	})
	require.NoError(t, err)
	require.Nil(t, fields)
}

func TestValidateCommand_InvalidEmailWhenProvided(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateCommand(notificationdto.CommandRequest{
		IdempotencyKey: "key-1",
		UserID:         "11111111-1111-1111-1111-111111111111",
		TemplateCode:   "withdrawal_approved",
		Contacts:       &notificationdto.Contacts{Email: "not-an-email"},
	})
	require.Error(t, err)
	require.Contains(t, fields, "contacts.email")
}

func TestValidateCommand_InvalidChannelWhenProvided(t *testing.T) {
	v := notificationvalidator.New(map[string]bool{"in_app": true})
	fields, err := v.ValidateCommand(notificationdto.CommandRequest{
		IdempotencyKey: "key-1",
		UserID:         "11111111-1111-1111-1111-111111111111",
		TemplateCode:   "withdrawal_approved",
		Channels:       []string{"sms"},
		Contacts:       &notificationdto.Contacts{},
	})
	require.Error(t, err)
	require.Contains(t, fields, "channels[0]")
}
