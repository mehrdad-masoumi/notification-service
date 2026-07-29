package notificationcontract

import (
	"context"

	notificationdto "notification-service/internal/notification/dto"
	"notification-service/internal/notification/entity"
)

//go:generate mockery --name=IFPublisher --inpackage --filename=mock_publisher.go --structname=MockPublisher
type IFPublisher interface {
	Publish(ctx context.Context, priority entity.Priority, job notificationdto.QueueJob) error
	Ping(ctx context.Context) error
}

type SendRequest struct {
	NotificationID string
	DeliveryID     string
	// IdempotencyKey is passed to the downstream provider so retries of the
	// same delivery attempt do not create duplicate sends on their side.
	IdempotencyKey string
	UserID         string
	Channel        string
	To             string
	Title          string
	Message        string
	ActionURL      string
	Payload        map[string]any
}

type SendResult struct {
	Provider  string
	MessageID string
	Meta      map[string]any
}

//go:generate mockery --name=IFProvider --inpackage --filename=mock_provider.go --structname=MockProvider
type IFProvider interface {
	Channel() string
	Name() string
	Send(ctx context.Context, req SendRequest) (SendResult, error)
}

//go:generate mockery --name=IFProviderRegistry --inpackage --filename=mock_registry.go --structname=MockProviderRegistry
type IFProviderRegistry interface {
	Get(channel string) (IFProvider, error)
}
