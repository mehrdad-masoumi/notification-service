package entity

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

type Channel string

const (
	ChannelInApp    Channel = "in_app"
	ChannelEmail    Channel = "email"
	ChannelSMS      Channel = "sms"
	ChannelWhatsApp Channel = "whatsapp"
	ChannelPush     Channel = "push"
)

type NotificationStatus string

const (
	StatusPending         NotificationStatus = "pending"
	StatusQueued          NotificationStatus = "queued"
	StatusProcessing      NotificationStatus = "processing"
	StatusSent            NotificationStatus = "sent"
	StatusPartiallyFailed NotificationStatus = "partially_failed"
	StatusFailed          NotificationStatus = "failed"
	StatusScheduled       NotificationStatus = "scheduled"
)

type DeliveryStatus string

const (
	DeliveryPending         DeliveryStatus = "pending"
	DeliverySending         DeliveryStatus = "sending"
	DeliverySent            DeliveryStatus = "sent"
	DeliveryDelivered       DeliveryStatus = "delivered"
	DeliveryFailed          DeliveryStatus = "failed"
	DeliveryPermanentFailed DeliveryStatus = "permanent_failed"
)

// OutboxStatus tracks the lifecycle of a notification_outbox row as it
// moves from the transactional write through to being published on the
// message broker (transactional outbox pattern).
type OutboxStatus string

const (
	OutboxPending    OutboxStatus = "pending"
	OutboxPublishing OutboxStatus = "publishing"
	OutboxPublished  OutboxStatus = "published"
	OutboxFailed     OutboxStatus = "failed"
)

const DefaultLocale = "fa"

type Notification struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	BatchID        *uuid.UUID
	Title          string
	Message        string
	Type           string
	Priority       Priority
	Payload        json.RawMessage
	ActionURL      *string
	Status         NotificationStatus
	ReadAt         *time.Time
	IdempotencyKey *string
	Channels       []Channel
	ScheduledAt    *time.Time
	CreatedBy      *uuid.UUID
	Email          *string
	Phone          *string
	TemplateCode   *string
	Locale         string
	Variables      json.RawMessage
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Delivery struct {
	ID             uuid.UUID
	NotificationID uuid.UUID
	Channel        Channel
	Provider       string
	Status         DeliveryStatus
	Attempts       int
	Error          *string
	SentAt         *time.Time
	DeliveredAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Template is an admin-managed, per (code, locale, channel) message template.
type Template struct {
	ID              uuid.UUID
	Code            string
	Locale          string
	Channel         Channel
	Subject         *string
	Body            string
	DefaultPriority Priority
	Enabled         bool
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// OutboxEvent is a transactional-outbox row: written in the same DB
// transaction as the notification/delivery rows it describes, later
// claimed and published to RabbitMQ by the outbox publisher process.
type OutboxEvent struct {
	ID          uuid.UUID
	AggregateID uuid.UUID
	DeliveryID  uuid.UUID
	EventType   string
	RoutingKey  string
	Payload     json.RawMessage
	Status      OutboxStatus
	Attempts    int
	AvailableAt time.Time
	LockedAt    *time.Time
	LockedBy    *string
	PublishedAt *time.Time
	LastError   *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BatchJobStatus tracks async (>N recipients) admin batch fan-out jobs.
type BatchJobStatus string

const (
	BatchJobPending    BatchJobStatus = "pending"
	BatchJobProcessing BatchJobStatus = "processing"
	BatchJobCompleted  BatchJobStatus = "completed"
	BatchJobFailed     BatchJobStatus = "failed"
)

type BatchRecipientStatus string

const (
	BatchRecipientPending  BatchRecipientStatus = "pending"
	BatchRecipientAccepted BatchRecipientStatus = "accepted"
	BatchRecipientFailed   BatchRecipientStatus = "failed"
)

type BatchJob struct {
	ID                  uuid.UUID
	Status              BatchJobStatus
	TemplateCode        string
	Locale              string
	Channels            []Channel
	Priority            Priority
	Variables           json.RawMessage
	ActionURL           *string
	ScheduledAt         *time.Time
	CreatedBy           *uuid.UUID
	TotalRecipients     int
	ProcessedRecipients int
	FailedRecipients    int
	LastError           *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type BatchJobRecipient struct {
	ID             uuid.UUID
	JobID          uuid.UUID
	UserID         uuid.UUID
	Status         BatchRecipientStatus
	NotificationID *uuid.UUID
	Error          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type BatchSummary struct {
	ID              uuid.UUID
	BatchID         *uuid.UUID
	Title           string
	Message         string
	Type            string
	Priority        Priority
	Channels        []Channel
	Status          NotificationStatus
	RecipientsCount int
	SuccessCount    int
	FailedCount     int
	CreatedBy       *uuid.UUID
	CreatedAt       time.Time
	ScheduledAt     *time.Time
}
