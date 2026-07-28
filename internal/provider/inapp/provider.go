package inappprovider

import (
	"context"
	"fmt"
	"time"

	notificationcontract "notification-service/internal/notification/contract"
)

// Provider marks in-app delivery as sent; persistence is already in DB.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Channel() string { return "in_app" }
func (p *Provider) Name() string    { return "postgres" }

func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	_ = req
	return notificationcontract.SendResult{
		Provider:  p.Name(),
		MessageID: fmt.Sprintf("inapp_%d", time.Now().UnixNano()),
	}, nil
}
