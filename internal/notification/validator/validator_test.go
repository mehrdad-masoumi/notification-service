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

func TestValidateAdminCreate_OK(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
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
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateAdminCreate(notificationdto.AdminCreateRequest{})
	require.Error(t, err)
	require.Contains(t, fields, "title")
	require.Contains(t, fields, "message")
	require.Contains(t, fields, "user_ids")
	require.Contains(t, fields, "channels")
}

func TestValidateInternalCreate_RequiresIdempotencyKey(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
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
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateAdminCreate(notificationdto.AdminCreateRequest{
		Title:    "t",
		Message:  "m",
		UserIDs:  []string{"11111111-1111-1111-1111-111111111111"},
		Channels: []string{"fax"},
	})
	require.Error(t, err)
	require.Contains(t, fields, "channels[0]")
}

func TestValidateChannels_DisabledByConfig(t *testing.T) {
	v := notificationvalidator.New(map[string]bool{"in_app": true, "email": true})
	fields, err := v.ValidateAdminCreate(notificationdto.AdminCreateRequest{
		Title:    "t",
		Message:  "m",
		UserIDs:  []string{"11111111-1111-1111-1111-111111111111"},
		Channels: []string{"sms"},
	})
	require.Error(t, err)
	require.Contains(t, fields, "channels[0]")
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

func TestValidateDirectCommand_OK(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateDirectCommand(notificationdto.DirectCommandRequest{
		IdempotencyKey: "key-1",
		TemplateCode:   "login_otp",
		Channel:        "email",
		Recipient:      "user@example.com",
	})
	require.NoError(t, err)
	require.Nil(t, fields)
}

func TestValidateDirectCommand_InvalidEmail(t *testing.T) {
	v := notificationvalidator.New(allEnabled)
	fields, err := v.ValidateDirectCommand(notificationdto.DirectCommandRequest{
		IdempotencyKey: "key-1",
		TemplateCode:   "login_otp",
		Channel:        "email",
		Recipient:      "not-an-email",
	})
	require.Error(t, err)
	require.Contains(t, fields, "recipient")
}

func TestValidateDirectCommand_DisabledChannel(t *testing.T) {
	v := notificationvalidator.New(map[string]bool{"in_app": true})
	fields, err := v.ValidateDirectCommand(notificationdto.DirectCommandRequest{
		IdempotencyKey: "key-1",
		TemplateCode:   "login_otp",
		Channel:        "sms",
		Recipient:      "+989120000000",
	})
	require.Error(t, err)
	require.Contains(t, fields, "channel")
}
