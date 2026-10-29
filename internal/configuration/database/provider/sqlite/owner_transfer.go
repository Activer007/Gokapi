package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/forceu/gokapi/internal/uploadquota"
)

// Check existence and deletion state on the connection holding BEGIN IMMEDIATE.
// Quota Apply separately verifies the current owner, size and request projection.
func checkOwnerTransfer(ctx context.Context, conn *sql.Conn, change uploadquota.Change) error {
	if change.Source == nil || change.Source.UserId <= 0 || change.Upload.UserID <= 0 ||
		change.Source.Id == "" || len(change.Source.Id) > uploadquota.MaxIDLength {
		return uploadquota.ErrInvalid
	}
	var exists bool
	if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM Users WHERE Id = ?)", change.Upload.UserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return uploadquota.ErrConflict
	}
	var pending any
	err := conn.QueryRowContext(ctx, "SELECT PendingDeletion FROM FileMetaData WHERE Id = ?", change.Source.Id).Scan(&pending)
	if errors.Is(err, sql.ErrNoRows) {
		return uploadquota.ErrConflict
	}
	if err != nil {
		return err
	}
	value, ok := pending.(int64)
	if !ok {
		return uploadquota.ErrInvalid
	}
	if value != 0 {
		return uploadquota.ErrConflict
	}
	return nil
}

// Owner transfer changes no other current metadata, blob reference or hotlink.
func patchFileOwner(ctx context.Context, conn *sql.Conn, change uploadquota.Change) error {
	result, err := conn.ExecContext(ctx, `UPDATE FileMetaData SET UserId = ?
		WHERE Id = ? AND UserId = ? AND PendingDeletion = 0`, change.Upload.UserID, change.Source.Id, change.Source.UserId)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return uploadquota.ErrConflict
	}
	return nil
}
