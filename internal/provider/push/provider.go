package pushprovider

import (
	"context"

	notificationcontract "notification-service/internal/notification/contract"
	providerrerrors "notification-service/internal/provider"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Channel() string { return "push" }
func (p *Provider) Name() string    { return "stub" }

func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	_ = req
	return notificationcontract.SendResult{}, providerrerrors.Permanent("push provider not implemented", nil)
}
