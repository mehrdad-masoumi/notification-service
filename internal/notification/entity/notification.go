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
