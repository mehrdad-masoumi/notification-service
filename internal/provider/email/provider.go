package emailprovider

import (
	"context"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
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
	timeout  time.Duration
}

func New(cfg config.Email) *Provider {
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Provider{
		host:     cfg.Host,
		port:     cfg.Port,
		username: cfg.Username,
		password: cfg.Password,
		from:     cfg.From,
		timeout:  timeout,
	}
}

func (p *Provider) Channel() string { return "email" }
func (p *Provider) Name() string    { return "smtp" }

func (p *Provider) Send(ctx context.Context, req notificationcontract.SendRequest) (notificationcontract.SendResult, error) {
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

	subject := sanitizeHeaderValue(req.Title)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr.String(), toAddr.String(), subject, req.Message)

	if err := p.sendWithTimeout(ctx, fromAddr.Address, toAddr.Address, []byte(body)); err != nil {
		return notificationcontract.SendResult{}, providerrerrors.Temporary("smtp send failed", err)
	}

	return notificationcontract.SendResult{
		Provider:  p.Name(),
		MessageID: fmt.Sprintf("email_%d", time.Now().UnixNano()),
	}, nil
}

// sanitizeHeaderValue strips CR/LF from a value destined for an SMTP
// header (e.g. Subject) to prevent header/SMTP injection via
// attacker-controlled template variables.
func sanitizeHeaderValue(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}

// sendWithTimeout mirrors smtp.SendMail but enforces the configured
// timeout on the underlying connection (net/smtp has no built-in timeout
// support) and honors context cancellation for the initial dial.
func (p *Provider) sendWithTimeout(ctx context.Context, from, to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", p.host, p.port)

	dialer := &net.Dialer{Timeout: p.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(p.timeout))

	client, err := smtp.NewClient(conn, p.host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if p.username != "" {
		auth := smtp.PlainAuth("", p.username, p.password, p.host)
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("auth: %w", err)
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return client.Quit()
}
