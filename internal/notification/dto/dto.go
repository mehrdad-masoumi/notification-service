package notificationdto

import (
	"encoding/json"
	"time"
)

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
	ID          string          `json:"id"`
	BatchID     string          `json:"batch_id,omitempty"`
	UserID      string          `json:"user_id"`
	Title       string          `json:"title"`
	Message     string          `json:"message"`
	Type        string          `json:"type"`
	Priority    string          `json:"priority"`
	Channels    []string        `json:"channels"`
	Status      string          `json:"status"`
	ActionURL   string          `json:"action_url,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	ScheduledAt *time.Time      `json:"scheduled_at,omitempty"`
	Deliveries  []DeliveryItem  `json:"deliveries"`
}

type QueueJob struct {
	NotificationID string `json:"notification_id"`
	DeliveryID     string `json:"delivery_id"`
	Channel        string `json:"channel"`
	Attempt        int    `json:"attempt"`
}
