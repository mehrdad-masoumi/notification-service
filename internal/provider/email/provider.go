package emailprovider

import (
	"context"
	"fmt"
	"net/mail"
	"net/smtp"
	"time"

	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	providerrerrors "notification-service/internal/provider"
)

type Provider struct {
	host     string
	port     int
	username string
	password string
	from     string
}

func New(cfg config.Email) *Provider {
	return &Provider{
		host:     cfg.Host,
		port:     cfg.Port,
		username: cfg.Username,
		password: cfg.Password,
		from:     cfg.From,
	}
}

func (p *Provider) Channel() string { return "email" }
func (p *Provider) Name() string    { return "smtp" }

func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
	_ = ctx
	if p.host == "" {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("email provider not configured", nil)
	}
	if req.To == "" {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("missing email recipient", nil)
	}

	fromAddr, err := mail.ParseAddress(p.from)
	if err != nil {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("invalid from address", err)
	}
	toAddr, err := mail.ParseAddress(req.To)
	if err != nil {
		return notificationcontract.SendResult{}, providerrerrors.Permanent("invalid to address", err)
	}

	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr.String(), toAddr.String(), req.Title, req.Message)

	addr := fmt.Sprintf("%s:%d", p.host, p.port)
	auth := smtp.PlainAuth("", p.username, p.password, p.host)

	if err := smtp.SendMail(addr, auth, fromAddr.Address, []string{toAddr.Address}, []byte(body)); err != nil {
		return notificationcontract.SendResult{}, providerrerrors.Temporary("smtp send failed", err)
	}

	return notificationcontract.SendResult{
		Provider:  p.Name(),
		MessageID: fmt.Sprintf("email_%d", time.Now().UnixNano()),
	}, nil
}
