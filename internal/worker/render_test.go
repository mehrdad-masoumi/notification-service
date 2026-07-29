package worker_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"notification-service/internal/notification/entity"
	notificationtemplate "notification-service/internal/notification/template"
)

func TestPerChannelTemplateRender_UsesChannelBody(t *testing.T) {
	vars := map[string]any{"code": "123456"}
	emailSubject, emailBody, err := notificationtemplate.RenderPair(
		"OTP {{code}}",
		"Your email code is {{code}}",
		vars,
	)
	require.NoError(t, err)
	require.Equal(t, "OTP 123456", emailSubject)
	require.Equal(t, "Your email code is 123456", emailBody)

	_, smsBody, err := notificationtemplate.RenderPair("", "SMS code: {{code}}", vars)
	require.NoError(t, err)
	require.Equal(t, "SMS code: 123456", smsBody)
	require.NotEqual(t, emailBody, smsBody)
}

func TestNotificationVariablesJSONRoundTrip(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"amount": "1200", "currency": "USDT"})
	require.NoError(t, err)
	var vars map[string]any
	require.NoError(t, json.Unmarshal(raw, &vars))
	require.Equal(t, "1200", vars["amount"])
	_ = entity.ChannelEmail
}
