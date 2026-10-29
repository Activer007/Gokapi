package sqlite

import (
	"context"
	"database/sql"
	"reflect"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// ApplyFileState compares the complete checked snapshot within the write
// transaction, then changes only the selected control fields. It never creates
// a row, accesses a blob, or changes hotlinks or logical quota state.
func (p DatabaseProvider) ApplyFileState(ctx context.Context, patch models.FileStatePatch) (models.File, bool, error) {
	if ctx == nil {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, false, err
	}
	if !validFileStatePatch(patch) {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	var updated models.File
	found := false
	err := p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		current, exists, err := readRecoveryFile(ctx, conn, patch.Expected.Id)
		if err != nil || !exists {
			return err
		}
		if !reflect.DeepEqual(current, patch.Expected) {
			return uploadquota.ErrConflict
		}
		var statement string
		var args []any
		switch patch.Operation {
		case models.FileStateExpire:
			current.ExpireAt, current.UnlimitedTime, current.PendingDeletion = 0, false, 0
			statement = "UPDATE FileMetaData SET ExpireAt = 0, UnlimitedTime = 0, PendingDeletion = 0 WHERE Id = ?"
			args = []any{current.Id}
		case models.FileStateSchedule:
			current.PendingDeletion = patch.PendingDeletion
			statement = "UPDATE FileMetaData SET PendingDeletion = ? WHERE Id = ?"
			args = []any{current.PendingDeletion, current.Id}
		case models.FileStateCancel:
			current.PendingDeletion = 0
			statement = "UPDATE FileMetaData SET PendingDeletion = 0 WHERE Id = ?"
			args = []any{current.Id}
		case models.FileStatePassword:
			current.PasswordHash = patch.PasswordHash
			statement = "UPDATE FileMetaData SET PasswordHash = ? WHERE Id = ?"
			args = []any{current.PasswordHash, current.Id}
		}
		result, err := conn.ExecContext(ctx, statement, args...)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return uploadquota.ErrConflict
		}
		updated, found = current, true
		return nil
	})
	if err != nil {
		return models.File{}, false, err
	}
	return updated, found, nil
}

func validFileStatePatch(patch models.FileStatePatch) bool {
	if !recoveryFileID.MatchString(patch.Expected.Id) {
		return false
	}
	switch patch.Operation {
	case models.FileStateExpire, models.FileStateCancel:
		return patch.PendingDeletion == 0 && patch.PasswordHash == ""
	case models.FileStateSchedule:
		return patch.PendingDeletion > 0 && patch.PasswordHash == ""
	case models.FileStatePassword:
		return patch.PendingDeletion == 0 && patch.PasswordHash != "" && len(patch.PasswordHash) <= uploadquota.MaxMetadataTextBytes
	default:
		return false
	}
}
