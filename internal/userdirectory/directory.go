package userdirectory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	providerrerrors "notification-service/internal/provider"
)

type HTTPDirectory struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewHTTP(cfg config.UserService) *HTTPDirectory {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &HTTPDirectory{
		baseURL: stringsTrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func stringsTrimRight(s, cut string) string {
	for len(s) > 0 && len(cut) > 0 && s[len(s)-1:] == cut {
		s = s[:len(s)-1]
	}
	return s
}

type contactResponse struct {
	Email string `json:"email"`
	Phone string `json:"phone"`
}

func (d *HTTPDirectory) Resolve(ctx context.Context, userID string) (notificationcontract.UserContacts, error) {
	if d.baseURL == "" {
		return notificationcontract.UserContacts{}, providerrerrors.Permanent("user directory not configured", nil)
	}
	url := fmt.Sprintf("%s/internal/users/%s/contacts", d.baseURL, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return notificationcontract.UserContacts{}, providerrerrors.Temporary("build user request", err)
	}
	if d.apiKey != "" {
		req.Header.Set("X-Internal-Api-Key", d.apiKey)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return notificationcontract.UserContacts{}, providerrerrors.Temporary("user directory request failed", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return notificationcontract.UserContacts{}, providerrerrors.Permanent("user not found", nil)
	}
	if resp.StatusCode >= 500 {
		return notificationcontract.UserContacts{}, providerrerrors.Temporary("user directory unavailable", nil)
	}
	if resp.StatusCode >= 400 {
		return notificationcontract.UserContacts{}, providerrerrors.Permanent("user directory rejected request", nil)
	}

	var body contactResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return notificationcontract.UserContacts{}, providerrerrors.Temporary("decode user contacts", err)
	}
	return notificationcontract.UserContacts{Email: body.Email, Phone: body.Phone}, nil
}

// NoopDirectory always returns empty contacts (used when user service is unset).
type NoopDirectory struct{}

func (NoopDirectory) Resolve(ctx context.Context, userID string) (notificationcontract.UserContacts, error) {
	_ = ctx
	_ = userID
	return notificationcontract.UserContacts{}, nil
}
