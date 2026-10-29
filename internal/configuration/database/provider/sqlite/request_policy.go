package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Activer007/Gokapi/internal/uploadquota"
)

// Read only the current numeric policy while BEGIN IMMEDIATE holds the same
// connection as the quota snapshot. Names and public capabilities are omitted.
func readRequestPolicy(ctx context.Context, conn *sql.Conn, requestID string) (uploadquota.RequestPolicy, error) {
	if len(requestID) == 0 || len(requestID) > uploadquota.MaxIDLength {
		return uploadquota.RequestPolicy{}, uploadquota.ErrInvalid
	}
	var values [4]any
	err := conn.QueryRowContext(ctx, "SELECT UserId, MaxFiles, MaxSize, Expiry FROM UploadRequests WHERE Id = ?", requestID).
		Scan(&values[0], &values[1], &values[2], &values[3])
	if errors.Is(err, sql.ErrNoRows) {
		return uploadquota.RequestPolicy{}, uploadquota.ErrNotFound
	}
	if err != nil {
		return uploadquota.RequestPolicy{}, err
	}
	var parsed [4]int64
	for i, value := range values {
		integer, ok := value.(int64)
		if !ok {
			return uploadquota.RequestPolicy{}, uploadquota.ErrInvalid
		}
		parsed[i] = integer
	}
	return uploadquota.RequestPolicy{UserID: parsed[0], MaxFiles: parsed[1], MaxSizeMB: parsed[2], Expiry: parsed[3]}, nil
}
