package whatsappprovider

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

func New(cfg config.WhatsApp) *Provider {
	return &Provider{enabled: cfg.Enabled, from: cfg.From, apiKey: cfg.APIKey}
}

func (p *Provider) Channel() string { return "whatsapp" }
func (p *Provider) Name() string    { return "noop" }

func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	if req.To == "" {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("missing whatsapp recipient", nil)
	}
	if !p.enabled {
		log.Printf("whatsapp noop: delivery_id=%s (recipient redacted)", req.DeliveryID)
		return notificationcontract.SendResult{
			Provider:  p.Name(),
			MessageID: fmt.Sprintf("wa_noop_%d", time.Now().UnixNano()),
		}, nil
	}
	_ = p.apiKey
	_ = p.from
	return notificationcontract.SendResult{}, providerrerrors.Permanent("whatsapp provider not implemented", nil)
}
