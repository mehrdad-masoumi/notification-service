package smsprovider

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

func New(cfg config.SMS) *Provider {
	return &Provider{enabled: cfg.Ready(), from: cfg.From, apiKey: cfg.APIKey}
}

func (p *Provider) Channel() string { return "sms" }
func (p *Provider) Name() string    { return "noop" }

// Send never reports success for a channel that is not actually
// configured: an unconfigured/disabled SMS provider is a permanent
// failure, not a silent success, so operators and callers see the real
// delivery state instead of a false "sent".
func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	if req.To == "" {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("missing phone recipient", nil)
	}
	if !p.enabled {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("sms provider disabled", nil)
	}
	_ = p.apiKey
	_ = p.from
	return notificationcontract.SendResult{}, providerrerrors.Permanent("sms provider not implemented", nil)
}
