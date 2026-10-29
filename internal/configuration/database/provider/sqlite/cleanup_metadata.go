package sqlite

import (
	"context"
	"database/sql"
	"reflect"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

const cleanupMetadataLimit = 16384

// ReadCleanupIDs returns the complete bounded scan or an error. No caller may
// interpret a truncated or failed scan as evidence that a blob is unreferenced.
func (p DatabaseProvider) ReadCleanupIDs(ctx context.Context) ([]string, error) {
	if ctx == nil {
		return nil, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.sqliteDb == nil {
		return nil, uploadquota.ErrSchema
	}
	rows, err := p.sqliteDb.QueryContext(ctx, `SELECT
		CASE WHEN typeof(Id) = 'text' AND length(CAST(Id AS BLOB)) BETWEEN 1 AND 128 THEN Id ELSE NULL END
		FROM FileMetaData ORDER BY Id LIMIT 16385`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if !recoveryFileID.MatchString(id) || len(ids) == cleanupMetadataLimit {
			return nil, uploadquota.ErrInvalid
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// HasBlobReference checks every locator, including references owned by another
// user. Display fields and encryption are deliberately not decoded here.
func (p DatabaseProvider) HasBlobReference(ctx context.Context, file models.File) (bool, error) {
	if ctx == nil || !validCleanupLocator(file) {
		return false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.sqliteDb == nil {
		return false, uploadquota.ErrSchema
	}
	rows, err := p.sqliteDb.QueryContext(ctx, `SELECT
		CASE WHEN typeof(Id) = 'text' AND length(CAST(Id AS BLOB)) BETWEEN 1 AND 128 THEN Id ELSE NULL END,
		CASE WHEN typeof(SHA1) = 'text' AND length(CAST(SHA1 AS BLOB)) BETWEEN 1 AND 128 THEN SHA1 ELSE NULL END,
		CASE WHEN typeof(AwsBucket) = 'text' AND length(CAST(AwsBucket AS BLOB)) <= 1024 THEN AwsBucket ELSE NULL END
		FROM FileMetaData LIMIT 16385`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found, count := false, 0
	for rows.Next() {
		var id, hash, bucket string
		if err := rows.Scan(&id, &hash, &bucket); err != nil {
			return false, err
		}
		count++
		if count > cleanupMetadataLimit || !recoveryFileID.MatchString(id) {
			return false, uploadquota.ErrInvalid
		}
		if id != file.Id && hash == file.SHA1 && bucket == file.AwsBucket {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return found, nil
}

// RetireCleanupFile removes only the exact row inspected by storage and its
// still-owned hotlink. sourceGone is storage's checked physical result, never
// an HTTP parameter. Physical I/O must finish before this short transaction.
func (p DatabaseProvider) RetireCleanupFile(ctx context.Context, expected models.File, now int64, sourceGone bool) (bool, error) {
	if ctx == nil || !validCleanupLocator(expected) || expected.UserId <= 0 || expected.SizeBytes < 0 || now <= 0 {
		return false, uploadquota.ErrInvalid
	}
	retired := false
	err := p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		current, found, err := readRecoveryFile(ctx, conn, expected.Id)
		if err != nil || !found {
			return err
		}
		if !reflect.DeepEqual(current, expected) || (!sourceGone && !cleanupMetadataDue(current, now)) {
			return uploadquota.ErrConflict
		}
		result, err := conn.ExecContext(ctx, "DELETE FROM FileMetaData WHERE Id = ?", current.Id)
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
		if current.HotlinkId != "" {
			if _, err := conn.ExecContext(ctx, "DELETE FROM Hotlinks WHERE Id = ? AND FileId = ?", current.HotlinkId, current.Id); err != nil {
				return err
			}
		}
		retired = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return retired, nil
}

func validCleanupLocator(file models.File) bool {
	return recoveryFileID.MatchString(file.Id) && len(file.SHA1) > 0 && len(file.SHA1) <= 128 && len(file.AwsBucket) <= 1024
}

func cleanupMetadataDue(file models.File, now int64) bool {
	return (!file.UnlimitedTime && file.ExpireAt < now) ||
		(!file.UnlimitedDownloads && file.DownloadsRemaining < 1) ||
		(file.PendingDeletion > 0 && file.PendingDeletion < now)
}
