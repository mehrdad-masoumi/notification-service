package application

import "time"

// Channel mirrors delivery channel wire values.
type Channel string

const (
	ChannelInApp    Channel = "in_app"
	ChannelEmail    Channel = "email"
	ChannelSMS      Channel = "sms"
	ChannelWhatsApp Channel = "whatsapp"
	ChannelPush     Channel = "push"
)

// Recipient is the caller-supplied contact snapshot.
type Recipient struct {
	UserID       string
	Email        string
	Phone        string
	DeviceTokens []string
	DisplayName  string
}

// SendNotificationCommand is the shared application command used by all
// transports (gRPC, RabbitMQ, Admin HTTP).
type SendNotificationCommand struct {
	MessageID      string
	IdempotencyKey string
	SourceService  string
	TemplateCode   string
	Locale         string
	Recipient      Recipient
	Channels       []Channel
	Variables      map[string]any
	Metadata       map[string]string
	ScheduledAt    *time.Time
	CorrelationID  string
	TraceID        string
	Priority       string
	ActionURL      string
	CreatedBy      string
}

// SendResult is returned after durable accept (not after delivery).
type SendResult struct {
	NotificationID string
	Status         string // accepted | scheduled | duplicate
	Duplicate      bool
	AcceptedAt     time.Time
	HTTPStatus     int
}
