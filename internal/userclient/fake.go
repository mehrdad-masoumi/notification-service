package userclient

import (
	"context"

	notificationcontract "notification-service/internal/notification/contract"
)

// Fake is an in-memory IFUserContacts implementation for tests.
type Fake struct {
	Contacts map[string]notificationcontract.Contacts
	Err      error
}

func NewFake() *Fake {
	return &Fake{Contacts: map[string]notificationcontract.Contacts{}}
}

func (f *Fake) ResolveContacts(ctx context.Context, userID string) (notificationcontract.Contacts, error) {
	_ = ctx
	if f.Err != nil {
		return notificationcontract.Contacts{}, f.Err
	}
	c, ok := f.Contacts[userID]
	if !ok {
		return notificationcontract.Contacts{}, nil
	}
	return c, nil
}
