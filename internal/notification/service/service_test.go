package notificationservice_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mehrdad-masoumi/go-packages/apperr"
	notificationdto "notification-service/internal/notification/dto"
	notificationservice "notification-service/internal/notification/service"
	notificationvalidator "notification-service/internal/notification/validator"
)

var allEnabled = map[string]bool{
	"in_app":   true,
	"email":    true,
	"sms":      true,
	"whatsapp": true,
	"push":     true,
}

func newTestService() *notificationservice.Service {
	return notificationservice.New(nil, notificationvalidator.New(allEnabled))
}

func TestAcceptCommand_ValidationError(t *testing.T) {
	svc := newTestService()
	_, code, err := svc.AcceptCommand(context.Background(), notificationdto.CommandRequest{})
	require.Error(t, err)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	var ve *apperr.Error
	require.ErrorAs(t, err, &ve)
	require.Contains(t, ve.Fields, "template_code")
	require.Contains(t, ve.Fields, "contacts")
}
