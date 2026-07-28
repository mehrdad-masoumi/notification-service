// Package userclient resolves user contact information (email/phone/locale/
// preferences) from the user-service over HTTP. Contact values are
// sensitive and must never be logged.
package userclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"notification-service/config"
	notificationcontract "notification-service/internal/notification/contract"
	providerrerrors "notification-service/internal/provider"
)

type Client struct {
	baseURL    string
	apiKey     string
	cfg        config.UserService
	httpClient *http.Client
}

func New(cfg config.UserService) *Client {
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		cfg:     cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout(),
		},
	}
}

type contactsResponse struct {
	Email         string          `json:"email"`
	Phone         string          `json:"phone"`
	Locale        string          `json:"locale"`
	EmailVerified bool            `json:"email_verified"`
	PhoneVerified bool            `json:"phone_verified"`
	Preferences   map[string]bool `json:"preferences"`
}

// ResolveContacts fetches contact details for userID. A 404 response is
// treated as permanent (the user does not exist / has no contacts); 5xx
// responses and network/timeout errors are treated as temporary and are
// safe to retry.
func (c *Client) ResolveContacts(ctx context.Context, userID string) (notificationcontract.Contacts, error) {
	if c.baseURL == "" {
		return notificationcontract.Contacts{}, providerrerrors.Permanent("user service not configured", nil)
	}

	url := c.baseURL + c.cfg.ContactsPath(userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return notificationcontract.Contacts{}, providerrerrors.Temporary("build user contacts request", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-Internal-Api-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return notificationcontract.Contacts{}, providerrerrors.Temporary("user service request failed", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return notificationcontract.Contacts{}, providerrerrors.Permanent("user not found", nil)
	case resp.StatusCode >= 500:
		return notificationcontract.Contacts{}, providerrerrors.Temporary("user service unavailable", nil)
	case resp.StatusCode >= 400:
		return notificationcontract.Contacts{}, providerrerrors.Permanent("user service rejected request", nil)
	}

	var body contactsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return notificationcontract.Contacts{}, providerrerrors.Temporary("decode user contacts", err)
	}

	return notificationcontract.Contacts{
		Email:         body.Email,
		Phone:         body.Phone,
		Locale:        body.Locale,
		EmailVerified: body.EmailVerified,
		PhoneVerified: body.PhoneVerified,
		Preferences:   body.Preferences,
	}, nil
}

// Noop always returns empty, unverified contacts. Used when the user
// service base URL is not configured (e.g. local dev without dependent
// services running).
type Noop struct{}

func (Noop) ResolveContacts(ctx context.Context, userID string) (notificationcontract.Contacts, error) {
	_ = ctx
	_ = userID
	return notificationcontract.Contacts{}, nil
}
