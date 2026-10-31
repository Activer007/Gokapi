package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// ReadUserUploadUsage validates the ledger and measures metadata on the same
// connection and deferred read transaction. No writer lock, quota mutation or
// reservation cleanup is needed for a display snapshot.
func (p DatabaseProvider) ReadUserUploadUsage(ctx context.Context, userID int) (uploadquota.Usage, error) {
	if ctx == nil || userID <= 0 {
		return uploadquota.Usage{}, uploadquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return uploadquota.Usage{}, err
	}
	if p.sqliteDb == nil {
		return uploadquota.Usage{}, uploadquota.ErrSchema
	}
	conn, err := p.sqliteDb.Conn(ctx)
	if err != nil {
		return uploadquota.Usage{}, err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return uploadquota.Usage{}, err
	}
	defer tx.Rollback()
	if _, err := readUploadQuota(ctx, tx); err != nil {
		return uploadquota.Usage{}, err
	}
	files, err := quotaMetadata(ctx, tx)
	if err != nil {
		return uploadquota.Usage{}, err
	}
	snapshot, err := uploadquota.Measure(files)
	if err != nil {
		return uploadquota.Usage{}, errors.Join(uploadquota.ErrInvalid, err)
	}
	if err := ctx.Err(); err != nil {
		return uploadquota.Usage{}, err
	}
	if err := tx.Commit(); err != nil {
		return uploadquota.Usage{}, err
	}
	return snapshot.Users[userID], nil
}
