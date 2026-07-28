package notificationdto

import (
	"encoding/json"
	"time"
)

// AdminCreateRequest is the legacy free-text admin fan-out request.
type AdminCreateRequest struct {
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	UserIDs     []string               `json:"user_ids"`
	Channels    []string               `json:"channels"`
	Priority    string                 `json:"priority"`
	Type        string                 `json:"type"`
	ActionURL   string                 `json:"action_url"`
	Payload     map[string]interface{} `json:"payload"`
	ScheduledAt *time.Time             `json:"scheduled_at"`
	Email       string                 `json:"email"`
	Phone       string                 `json:"phone"`
}

// InternalCreateRequest is the deprecated free-text internal request.
// Kept working for backward compatibility; new integrations should use
// CommandRequest against the /internal/v1/notifications endpoint.
type InternalCreateRequest struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	UserID         string                 `json:"user_id"`
	Title          string                 `json:"title"`
	Message        string                 `json:"message"`
	Type           string                 `json:"type"`
	Channels       []string               `json:"channels"`
	Priority       string                 `json:"priority"`
	ActionURL      string                 `json:"action_url"`
	Payload        map[string]interface{} `json:"payload"`
	ScheduledAt    *time.Time             `json:"scheduled_at"`
	Email          string                 `json:"email"`
	Phone          string                 `json:"phone"`
}

// CommandRequest is the v1 template-driven notification command.
type CommandRequest struct {
	IdempotencyKey string         `json:"idempotency_key"`
	UserID         string         `json:"user_id"`
	TemplateCode   string         `json:"template_code"`
	Locale         string         `json:"locale"`
	Channels       []string       `json:"channels"`
	Priority       string         `json:"priority"`
	Variables      map[string]any `json:"variables"`
	ActionURL      string         `json:"action_url"`
	ScheduledAt    *time.Time     `json:"scheduled_at"`
}

// DirectCommandRequest sends a single-channel notification to an explicit
// recipient (email/phone), bypassing user lookup entirely.
type DirectCommandRequest struct {
	IdempotencyKey string         `json:"idempotency_key"`
	TemplateCode   string         `json:"template_code"`
	Locale         string         `json:"locale"`
	Channel        string         `json:"channel"`
	Recipient      string         `json:"recipient"`
	Variables      map[string]any `json:"variables"`
	Priority       string         `json:"priority"`
}

type AcceptedResponse struct {
	ID      string `json:"id,omitempty"`
	BatchID string `json:"batch_id,omitempty"`
	Status  string `json:"status"`
}

type UserNotificationItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Type      string    `json:"type"`
	ActionURL string    `json:"action_url,omitempty"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

type ListMeta struct {
	Page    int   `json:"page"`
	PerPage int   `json:"per_page"`
	Total   int64 `json:"total"`
}

type UserListResponse struct {
	Data []UserNotificationItem `json:"data"`
	Meta ListMeta               `json:"meta"`
}

type UnreadCountResponse struct {
	Count int64 `json:"count"`
}

type AdminListItem struct {
	ID              string     `json:"id"`
	BatchID         string     `json:"batch_id,omitempty"`
	Title           string     `json:"title"`
	Message         string     `json:"message"`
	Type            string     `json:"type"`
	Priority        string     `json:"priority"`
	Channels        []string   `json:"channels"`
	Status          string     `json:"status"`
	RecipientsCount int        `json:"recipients_count"`
	SuccessCount    int        `json:"success_count"`
	FailedCount     int        `json:"failed_count"`
	CreatedBy       string     `json:"created_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ScheduledAt     *time.Time `json:"scheduled_at,omitempty"`
}

type AdminListResponse struct {
	Data []AdminListItem `json:"data"`
	Meta ListMeta        `json:"meta"`
}

type DeliveryItem struct {
	ID        string     `json:"id"`
	Channel   string     `json:"channel"`
	Provider  string     `json:"provider"`
	Status    string     `json:"status"`
	Attempts  int        `json:"attempts"`
	Error     string     `json:"error,omitempty"`
	SentAt    *time.Time `json:"sent_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type AdminDetailResponse struct {
	ID           string          `json:"id"`
	BatchID      string          `json:"batch_id,omitempty"`
	UserID       string          `json:"user_id"`
	Title        string          `json:"title"`
	Message      string          `json:"message"`
	Type         string          `json:"type"`
	Priority     string          `json:"priority"`
	Channels     []string        `json:"channels"`
	Status       string          `json:"status"`
	ActionURL    string          `json:"action_url,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	TemplateCode string          `json:"template_code,omitempty"`
	Locale       string          `json:"locale,omitempty"`
	CreatedBy    string          `json:"created_by,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	ScheduledAt  *time.Time      `json:"scheduled_at,omitempty"`
	Deliveries   []DeliveryItem  `json:"deliveries"`
}

// AdminBatchMember is a single recipient's notification within a batch.
type AdminBatchMember struct {
	ID         string         `json:"id"`
	UserID     string         `json:"user_id"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	ReadAt     *time.Time     `json:"read_at,omitempty"`
	Deliveries []DeliveryItem `json:"deliveries,omitempty"`
}

// AdminBatchDetail describes a batch (a single admin/broadcast create call)
// and all member notifications within it.
type AdminBatchDetail struct {
	BatchID         string             `json:"batch_id"`
	Title           string             `json:"title"`
	Message         string             `json:"message"`
	Priority        string             `json:"priority"`
	Channels        []string           `json:"channels"`
	RecipientsCount int                `json:"recipients_count"`
	SuccessCount    int                `json:"success_count"`
	FailedCount     int                `json:"failed_count"`
	CreatedAt       time.Time          `json:"created_at"`
	ScheduledAt     *time.Time         `json:"scheduled_at,omitempty"`
	Members         []AdminBatchMember `json:"members"`
}

type QueueJob struct {
	NotificationID string `json:"notification_id"`
	DeliveryID     string `json:"delivery_id"`
	Channel        string `json:"channel"`
	Attempt        int    `json:"attempt"`
}

// --- Template CRUD DTOs ---

type TemplateCreateRequest struct {
	Code            string `json:"code"`
	Locale          string `json:"locale"`
	Channel         string `json:"channel"`
	Subject         string `json:"subject"`
	Body            string `json:"body"`
	DefaultPriority string `json:"default_priority"`
	Enabled         *bool  `json:"enabled"`
}

type TemplateUpdateRequest struct {
	Subject         *string `json:"subject"`
	Body            *string `json:"body"`
	DefaultPriority *string `json:"default_priority"`
}

type TemplateStatusRequest struct {
	Enabled bool `json:"enabled"`
}

type TemplateResponse struct {
	ID              string    `json:"id"`
	Code            string    `json:"code"`
	Locale          string    `json:"locale"`
	Channel         string    `json:"channel"`
	Subject         string    `json:"subject,omitempty"`
	Body            string    `json:"body"`
	DefaultPriority string    `json:"default_priority"`
	Enabled         bool      `json:"enabled"`
	Version         int       `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type TemplateListFilter struct {
	Code    string
	Locale  string
	Channel string
	Enabled *bool
	Page    int
	PerPage int
}

type TemplateListResponse struct {
	Data []TemplateResponse `json:"data"`
	Meta ListMeta           `json:"meta"`
}
