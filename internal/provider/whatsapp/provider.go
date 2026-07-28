package whatsappprovider

import (
	"context"

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
	return &Provider{enabled: cfg.Ready(), from: cfg.From, apiKey: cfg.APIKey}
}

func (p *Provider) Channel() string { return "whatsapp" }
func (p *Provider) Name() string    { return "noop" }

// Send never reports success for a channel that is not actually
// configured: an unconfigured/disabled WhatsApp provider is a permanent
// failure, not a silent success.
func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	if req.To == "" {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("missing whatsapp recipient", nil)
	}
	if !p.enabled {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("whatsapp provider disabled", nil)
	}
	_ = p.apiKey
	_ = p.from
	return notificationcontract.SendResult{}, providerrerrors.Permanent("whatsapp provider not implemented", nil)
}
