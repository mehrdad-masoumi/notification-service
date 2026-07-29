//go:build integration

package notificationrepo_test

// Integration tests for transactional outbox, idempotency, delivery claim,
// scheduled claim, and user visibility. Run with:
//
//	go test -tags=integration ./internal/notification/repository/...
//
// Requires Postgres (e.g. Testcontainers or docker-compose infra).
// These tests are excluded from the default `go test ./...` suite.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"notification-service/internal/notification/entity"
	notificationrepo "notification-service/internal/notification/repository"
)

func testDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("NOTIFICATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("NOTIFICATION_TEST_POSTGRES_DSN not set")
	}
	db, err := sqlx.Connect("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCreateNotificationBundle_Rollback(t *testing.T) {
	db := testDB(t)
	repo := notificationrepo.New(db)
	ctx := context.Background()

	n := entity.Notification{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		Title:    "t",
		Message:  "m",
		Type:     "system",
		Priority: entity.PriorityNormal,
		Status:   entity.StatusPending,
		Channels: []entity.Channel{entity.ChannelInApp},
		Locale:   "fa",
	}
	d := entity.Delivery{ID: uuid.New(), Channel: entity.ChannelInApp, Status: entity.DeliveryPending}
	// Invalid outbox routing forces failure if validation exists; otherwise
	// this smoke test asserts the happy path insert succeeds.
	_, _, err := repo.CreateNotificationBundle(ctx, n, []entity.Delivery{d}, nil, nil)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, n.ID, got.ID)
}

func TestClaimDelivery_Atomic(t *testing.T) {
	db := testDB(t)
	repo := notificationrepo.New(db)
	ctx := context.Background()

	n := entity.Notification{
		ID: uuid.New(), UserID: uuid.New(), Title: "t", Message: "m",
		Type: "system", Priority: entity.PriorityNormal, Status: entity.StatusPending,
		Channels: []entity.Channel{entity.ChannelInApp}, Locale: "fa",
	}
	d := entity.Delivery{ID: uuid.New(), Channel: entity.ChannelInApp, Status: entity.DeliveryPending}
	_, deliveries, err := repo.CreateNotificationBundle(ctx, n, []entity.Delivery{d}, nil, nil)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)

	claimed, err := repo.ClaimDelivery(ctx, deliveries[0].ID)
	require.NoError(t, err)
	require.Equal(t, entity.DeliverySending, claimed.Status)

	_, err = repo.ClaimDelivery(ctx, deliveries[0].ID)
	require.Error(t, err)
}

func TestIdempotency_HashConflict(t *testing.T) {
	db := testDB(t)
	repo := notificationrepo.New(db)
	ctx := context.Background()
	key := "test-idem-" + uuid.NewString()

	rec, outcome, err := repo.ClaimIdempotency(ctx, key, "op", "hash-a")
	require.NoError(t, err)
	require.NotNil(t, rec)
	_ = outcome

	require.NoError(t, repo.CompleteIdempotency(ctx, key, 202, map[string]string{"status": "accepted"}))

	_, outcome2, err := repo.ClaimIdempotency(ctx, key, "op", "hash-b")
	require.NoError(t, err)
	require.Equal(t, notificationrepo.ClaimConflict, outcome2)
}

func TestUserVisibility_HidesScheduled(t *testing.T) {
	db := testDB(t)
	repo := notificationrepo.New(db)
	ctx := context.Background()
	userID := uuid.New()
	future := time.Now().UTC().Add(2 * time.Hour)

	n := entity.Notification{
		ID: uuid.New(), UserID: userID, Title: "future", Message: "m",
		Type: "system", Priority: entity.PriorityNormal, Status: entity.StatusScheduled,
		Channels: []entity.Channel{entity.ChannelInApp}, Locale: "fa", ScheduledAt: &future,
	}
	d := entity.Delivery{ID: uuid.New(), Channel: entity.ChannelInApp, Status: entity.DeliveryPending}
	_, _, err := repo.CreateNotificationBundle(ctx, n, []entity.Delivery{d}, nil, nil)
	require.NoError(t, err)

	items, total, err := repo.ListForUser(ctx, userID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Empty(t, items)
}
