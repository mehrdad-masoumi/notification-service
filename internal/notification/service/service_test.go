package notificationservice_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mehrdad-masoumi/go-packages/apperr"
	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
	notificationservice "notification-service/internal/notification/service"
	notificationvalidator "notification-service/internal/notification/validator"
)

type mockPublisher struct {
	jobs []notificationdto.QueueJob
}

func (m *mockPublisher) Publish(ctx context.Context, priority entity.Priority, job notificationdto.QueueJob) error {
	_ = ctx
	_ = priority
	m.jobs = append(m.jobs, job)
	return nil
}

func (m *mockPublisher) Ping(ctx context.Context) error {
	_ = ctx
	return nil
}

func TestAdminCreate_ValidationError(t *testing.T) {
	svc := notificationservice.New(nil, notificationvalidator.New(), &mockPublisher{})
	_, err := svc.AdminCreate(context.Background(), notificationdto.AdminCreateRequest{}, "11111111-1111-1111-1111-111111111111")
	require.Error(t, err)
	var ve *apperr.Error
	require.ErrorAs(t, err, &ve)
	require.Contains(t, ve.Fields, "title")
}

func TestInternalCreate_ValidationError(t *testing.T) {
	svc := notificationservice.New(nil, notificationvalidator.New(), &mockPublisher{})
	_, _, err := svc.InternalCreate(context.Background(), notificationdto.InternalCreateRequest{
		Title: "t", Message: "m", UserID: "bad", Channels: []string{"in_app"},
	})
	require.Error(t, err)
	var ve *apperr.Error
	require.ErrorAs(t, err, &ve)
}
