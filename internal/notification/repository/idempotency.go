package notificationrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// staleProcessingTimeout bounds how long an idempotency key may stay in
// 'processing' before a new request with the same key is allowed to
// reclaim and retry it (e.g. after a crash mid-request).
const staleProcessingTimeout = 2 * time.Minute

type IdempotencyRecord struct {
	Key          string
	Operation    string
	RequestHash  string
	Status       string
	ResponseCode int
	ResponseBody json.RawMessage
}

type ClaimOutcome int

const (
	// ClaimAcquired means the caller now owns this key and must call
	// CompleteIdempotency or FailIdempotency when done.
	ClaimAcquired ClaimOutcome = iota
	// ClaimReplay means a prior call with the same key+hash already
	// succeeded; Record.ResponseCode/ResponseBody hold the response to
	// replay verbatim.
	ClaimReplay
	// ClaimConflict means the key was reused with a different request
	// payload (hash mismatch); callers should return HTTP 409.
	ClaimConflict
	// ClaimInProgress means another in-flight request currently owns the
	// key; callers should return HTTP 409/425 without side effects.
	ClaimInProgress
)

type idempotencyRow struct {
	Key          string         `db:"ide_key"`
	Operation    string         `db:"operation"`
	RequestHash  string         `db:"request_hash"`
	Status       string         `db:"status"`
	ResponseCode sql.NullInt64  `db:"response_code"`
	ResponseBody sql.NullString `db:"response_body"`
}

func mapIdempotencyRow(row idempotencyRow) IdempotencyRecord {
	rec := IdempotencyRecord{
		Key:         row.Key,
		Operation:   row.Operation,
		RequestHash: row.RequestHash,
		Status:      row.Status,
	}
	if row.ResponseCode.Valid {
		rec.ResponseCode = int(row.ResponseCode.Int64)
	}
	if row.ResponseBody.Valid {
		rec.ResponseBody = json.RawMessage(row.ResponseBody.String)
	}
	return rec
}

const idempotencySelectCols = `ide_key, operation, request_hash, status, response_code, response_body`

// ClaimIdempotency atomically claims an idempotency key for processing.
// It is safe under concurrent requests: exactly one caller gets
// ClaimAcquired for a given (key) at a time; others get Replay/Conflict/
// InProgress depending on the existing row's state.
func (r *Repository) ClaimIdempotency(ctx context.Context, key, operation, requestHash string) (IdempotencyRecord, ClaimOutcome, error) {
	var row idempotencyRow

	err := r.db.GetContext(ctx, &row, `
		INSERT INTO idempotency_keys (ide_key, operation, request_hash, status)
		VALUES ($1, $2, $3, 'processing')
		ON CONFLICT (ide_key) DO NOTHING
		RETURNING `+idempotencySelectCols,
		key, operation, requestHash)
	if err == nil {
		return mapIdempotencyRow(row), ClaimAcquired, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return IdempotencyRecord{}, 0, err
	}

	// Row already exists. Try to reclaim it if it has been stuck in
	// 'processing' for too long (e.g. the owning request crashed).
	err = r.db.GetContext(ctx, &row, `
		UPDATE idempotency_keys
		SET request_hash = $2, status = 'processing', response_code = NULL, response_body = NULL, updated_at = NOW()
		WHERE ide_key = $1 AND status = 'processing' AND updated_at < NOW() - ($3 * INTERVAL '1 second')
		RETURNING `+idempotencySelectCols,
		key, requestHash, int64(staleProcessingTimeout.Seconds()))
	if err == nil {
		return mapIdempotencyRow(row), ClaimAcquired, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return IdempotencyRecord{}, 0, err
	}

	// Not claimable right now; fetch current state to classify the outcome.
	err = r.db.GetContext(ctx, &row, `
		SELECT `+idempotencySelectCols+` FROM idempotency_keys WHERE ide_key = $1`, key)
	if err != nil {
		return IdempotencyRecord{}, 0, err
	}
	rec := mapIdempotencyRow(row)

	if rec.RequestHash != requestHash {
		return rec, ClaimConflict, nil
	}
	switch rec.Status {
	case "succeeded":
		return rec, ClaimReplay, nil
	case "processing":
		return rec, ClaimInProgress, nil
	case "failed":
		// Prior attempt failed; reclaim so the caller can retry with the
		// same key+hash. Do not ClaimReplay — failed rows have no response.
		err = r.db.GetContext(ctx, &row, `
			UPDATE idempotency_keys
			SET request_hash = $2, status = 'processing', response_code = NULL, response_body = NULL, updated_at = NOW()
			WHERE ide_key = $1 AND status = 'failed'
			RETURNING `+idempotencySelectCols,
			key, requestHash)
		if err == nil {
			return mapIdempotencyRow(row), ClaimAcquired, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return IdempotencyRecord{}, 0, err
		}
		// Lost the race; re-classify.
		err = r.db.GetContext(ctx, &row, `
			SELECT `+idempotencySelectCols+` FROM idempotency_keys WHERE ide_key = $1`, key)
		if err != nil {
			return IdempotencyRecord{}, 0, err
		}
		rec = mapIdempotencyRow(row)
		if rec.RequestHash != requestHash {
			return rec, ClaimConflict, nil
		}
		if rec.Status == "succeeded" {
			return rec, ClaimReplay, nil
		}
		return rec, ClaimInProgress, nil
	default:
		return rec, ClaimInProgress, nil
	}
}

// CompleteIdempotency stores the successful response for later replay.
// Only meaningful for a key this process previously acquired via
// ClaimIdempotency (ClaimAcquired).
func (r *Repository) CompleteIdempotency(ctx context.Context, key string, code int, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET status = 'succeeded', response_code = $2, response_body = $3, updated_at = NOW()
		WHERE ide_key = $1`, key, code, b)
	return err
}

// FailIdempotency releases a claimed key so a future retry with the same
// key/hash can attempt processing again.
func (r *Repository) FailIdempotency(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE idempotency_keys SET status = 'failed', updated_at = NOW() WHERE ide_key = $1`, key)
	return err
}

// CleanupExpired deletes idempotency records past their expiry, keeping the
// table bounded.
func (r *Repository) CleanupExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
