package smsprovider

import (
	"context"
	"fmt"
	"log"
	"time"

	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	providerrerrors "notification-service/internal/provider"
)

type Provider struct {
	enabled bool
	from    string
	apiKey  string
}

func New(cfg config.SMS) *Provider {
	return &Provider{enabled: cfg.Enabled, from: cfg.From, apiKey: cfg.APIKey}
}

func (p *Provider) Channel() string { return "sms" }
func (p *Provider) Name() string    { return "noop" }

func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	if req.To == "" {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("missing phone recipient", nil)
	}
	if !p.enabled {
		log.Printf("sms noop: delivery_id=%s channel=sms (recipient redacted)", req.DeliveryID)
		return notificationcontract.SendResult{
			Provider:  p.Name(),
			MessageID: fmt.Sprintf("sms_noop_%d", time.Now().UnixNano()),
		}, nil
	}
	_ = p.apiKey
	_ = p.from
	return notificationcontract.SendResult{}, providerrerrors.Permanent("sms provider not implemented", nil)
}
