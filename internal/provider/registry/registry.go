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

// New registers only the providers that are actually usable:
//   - in_app and email are always registered (email reports a permanent
//     "not configured" error per-send if SMTP host is unset, since a
//     notification's channel list may still legitimately include email).
//   - sms/whatsapp/push are registered only when their config is Ready();
//     otherwise Get(channel) returns an error and the worker fails those
//     deliveries permanently instead of silently dropping or "sending" them.
func New(cfg config.Config) *Registry {
	r := &Registry{providers: map[string]notificationcontract.IFProvider{}}
	r.Register(inappprovider.New())
	r.Register(emailprovider.New(cfg.Email))
	if cfg.SMS.Ready() {
		r.Register(smsprovider.New(cfg.SMS))
	}
	if cfg.WhatsApp.Ready() {
		r.Register(whatsappprovider.New(cfg.WhatsApp))
	}
	if cfg.Push.Ready() {
		r.Register(pushprovider.New())
	}
	return r
}

func (r *Registry) Register(p notificationcontract.IFProvider) {
	r.providers[p.Channel()] = p
}

func (r *Registry) Get(channel string) (notificationcontract.IFProvider, error) {
	p, ok := r.providers[channel]
	if !ok {
		return nil, fmt.Errorf("provider not registered/ready for channel %s", channel)
	}
	return p, nil
}
