package worker_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	notificationcontract "notification-service/internal/notification/contract"
	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
	providerrerrors "notification-service/internal/provider"
)

type stubProvider struct {
	ch   string
	name string
	err  error
}

func (s stubProvider) Channel() string { return s.ch }
func (s stubProvider) Name() string    { return s.name }
func (s stubProvider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	_ = req
	if s.err != nil {
		return notificationcontract.SendResult{}, s.err
	}
	return notificationcontract.SendResult{Provider: s.name, MessageID: "1"}, nil
}

type stubRegistry struct {
	p notificationcontract.IFProvider
}

func (s stubRegistry) Get(channel string) (notificationcontract.IFProvider, error) {
	_ = channel
	return s.p, nil
}

func TestTemporaryVsPermanentClassification(t *testing.T) {
	temp := providerrerrors.Temporary("timeout", nil)
	perm := providerrerrors.Permanent("bad address", nil)
	require.True(t, providerrerrors.IsTemporary(temp))
	require.False(t, providerrerrors.IsTemporary(perm))
	require.True(t, providerrerrors.IsPermanent(perm))
	require.False(t, providerrerrors.IsPermanent(temp))
}

func TestQueueJobShape(t *testing.T) {
	job := notificationdto.QueueJob{
		NotificationID: uuid.New().String(),
		DeliveryID:     uuid.New().String(),
		Channel:        string(entity.ChannelInApp),
		Attempt:        0,
	}
	require.NotEmpty(t, job.NotificationID)
	require.Equal(t, "in_app", job.Channel)
}

func TestStubProviderSend(t *testing.T) {
	p := stubProvider{ch: "in_app", name: "postgres"}
	res, err := p.Send(context.Background(), notificationcontract.SendRequest{})
	require.NoError(t, err)
	require.Equal(t, "postgres", res.Provider)

	p.err = providerrerrors.Temporary("fail", nil)
	_, err = p.Send(context.Background(), notificationcontract.SendRequest{})
	require.True(t, providerrerrors.IsTemporary(err))
}
