package providerregistry

import (
	"fmt"

	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	emailprovider "notification-service/internal/provider/email"
	inappprovider "notification-service/internal/provider/inapp"
	pushprovider "notification-service/internal/provider/push"
	smsprovider "notification-service/internal/provider/sms"
	whatsappprovider "notification-service/internal/provider/whatsapp"
)

type Registry struct {
	providers map[string]notificationcontract.IFProvider
}

func New(cfg config.Config) *Registry {
	r := &Registry{providers: map[string]notificationcontract.IFProvider{}}
	r.Register(emailprovider.New(cfg.Email))
	r.Register(smsprovider.New(cfg.SMS))
	r.Register(whatsappprovider.New(cfg.WhatsApp))
	r.Register(inappprovider.New())
	r.Register(pushprovider.New())
	return r
}

func (r *Registry) Register(p notificationcontract.IFProvider) {
	r.providers[p.Channel()] = p
}

func (r *Registry) Get(channel string) (notificationcontract.IFProvider, error) {
	p, ok := r.providers[channel]
	if !ok {
		return nil, fmt.Errorf("provider not found for channel %s", channel)
	}
	return p, nil
}
