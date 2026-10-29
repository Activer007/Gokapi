package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// ApplyUploadQuota atomically updates the quota ledger and optional metadata.
func (p DatabaseProvider) ApplyUploadQuota(ctx context.Context, change uploadquota.Change) (uploadquota.Reservation, error) {
	var record uploadquota.Reservation
	err := p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		ledger, err := readUploadQuota(ctx, conn)
		if err != nil {
			return err
		}
		if change.Action == uploadquota.DuplicateMetadata {
			if change.Source == nil || change.Source.UserId <= 0 {
				return uploadquota.ErrInvalid
			}
			current, err := readDuplicateSource(ctx, conn, change.Source.Id)
			if err != nil {
				return err
			}
			if err := uploadquota.CheckDuplicateSource(*change.Source, current, change.Now); err != nil {
				return err
			}
		}
		if change.Action == uploadquota.TransferOwner {
			if err := checkOwnerTransfer(ctx, conn, change); err != nil {
				return err
			}
		}
		if change.Action == uploadquota.ReplaceMetadata {
			if err := checkReplacementMetadata(ctx, conn, change); err != nil {
				return err
			}
		}
		effective := change
		if change.RequiresRequestPolicy() {
			policy, err := readRequestPolicy(ctx, conn, change.Upload.RequestID)
			if err != nil {
				return err
			}
			effective, err = uploadquota.ApplyRequestPolicy(ledger, change, policy)
			if err != nil {
				return err
			}
		}
		var files map[string]models.File
		// Cleanup must remain possible when unrelated metadata is malformed.
		// These terminal transitions never admit new uploads or write metadata;
		// Apply validates their reservation scope independently.
		if change.Action != uploadquota.Abort && change.Action != uploadquota.ReleaseStaging {
			files, err = quotaMetadata(ctx, conn)
			if err != nil {
				return err
			}
		}
		next, result, metadata, err := uploadquota.Apply(ledger, files, effective)
		if err != nil {
			return err
		}
		encoded, err := uploadquota.Encode(next)
		if err != nil {
			return err
		}
		if change.Action == uploadquota.TransferOwner {
			if err := patchFileOwner(ctx, conn, change); err != nil {
				return err
			}
		}
		if change.Action == uploadquota.ReplaceMetadata {
			if err := registerReplacementBlob(ctx, conn, change); err != nil {
				return err
			}
			if err := patchReplacementMetadata(ctx, conn, change); err != nil {
				return err
			}
		}
		if metadata != nil {
			if err := saveQuotaHotlink(ctx, conn, *metadata); err != nil {
				return err
			}
			if err := saveMetaData(ctx, conn, *metadata); err != nil {
				return err
			}
		}
		if err := writeUploadQuota(ctx, conn, encoded); err != nil {
			return err
		}
		record = result
		return nil
	})
	if err != nil {
		return uploadquota.Reservation{}, err
	}
	return record, nil
}

// saveQuotaHotlink shares the metadata/ledger transaction. An existing mapping
// may belong to this file, but a different target must never be overwritten.
func saveQuotaHotlink(ctx context.Context, conn *sql.Conn, file models.File) error {
	if file.HotlinkId == "" {
		return nil
	}
	var target string
	err := conn.QueryRowContext(ctx, "SELECT FileId FROM Hotlinks WHERE Id = ?", file.HotlinkId).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = conn.ExecContext(ctx, "INSERT INTO Hotlinks (Id, FileId) VALUES (?, ?)", file.HotlinkId, file.Id)
		return err
	}
	if err != nil {
		return err
	}
	if target != file.Id {
		return uploadquota.ErrConflict
	}
	return nil
}

// ReadUploadQuota returns the persisted quota ledger.
func (p DatabaseProvider) ReadUploadQuota(ctx context.Context) (uploadquota.Ledger, error) {
	if ctx == nil {
		return uploadquota.Ledger{}, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return uploadquota.Ledger{}, err
	}
	if p.sqliteDb == nil {
		return uploadquota.Ledger{}, uploadquota.ErrSchema
	}
	return readUploadQuota(ctx, p.sqliteDb)
}

// ImportUploadQuota imports an offline ledger into an empty destination.
func (p DatabaseProvider) ImportUploadQuota(ctx context.Context, ledger uploadquota.Ledger) error {
	encoded, err := uploadquota.Encode(ledger)
	if err != nil {
		return err
	}
	if ledger.HasOutstanding() {
		return uploadquota.ErrConflict
	}
	return p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		current, err := readUploadQuota(ctx, conn)
		if err != nil {
			return err
		}
		if len(current.Records) != 0 {
			return uploadquota.ErrConflict
		}
		return writeUploadQuota(ctx, conn, encoded)
	})
}

// CheckEmptyForMigration checks every application table, including orphan rows
// not reachable from user or file listings. Migration is an offline operation;
// this transaction verifies one consistent destination snapshot without resets.
func (p DatabaseProvider) CheckEmptyForMigration(ctx context.Context) error {
	return p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		ledger, err := readUploadQuota(ctx, conn)
		if err != nil {
			return err
		}
		if len(ledger.Records) != 0 {
			return uploadquota.ErrConflict
		}
		candidates, err := readBlobCandidates(ctx, conn)
		if err != nil {
			return err
		}
		if len(candidates) != 0 {
			return uploadquota.ErrConflict
		}
		// These are fixed schema identifiers, never caller-provided SQL.
		for _, table := range [...]string{"ApiKeys", "E2EConfig", "FileMetaData", "Hotlinks", "Sessions", "Users", "UploadRequests", "Statistics"} {
			var exists bool
			if err := conn.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" LIMIT 1)").Scan(&exists); err != nil {
				return fmt.Errorf("check migration destination %s: %w", table, err)
			}
			if exists {
				return uploadquota.ErrConflict
			}
		}
		return nil
	})
}

type quotaRowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readUploadQuota(ctx context.Context, query quotaRowQuerier) (uploadquota.Ledger, error) {
	var length int64
	var encoded []byte
	// CAST ensures byte length even if a corrupted database stores TEXT in the
	// BLOB column. Oversized values never cross the SQL/Go boundary.
	err := query.QueryRowContext(ctx, `SELECT length(CAST(State AS BLOB)),
		CASE WHEN length(CAST(State AS BLOB)) <= ? THEN CAST(State AS BLOB) ELSE NULL END
		FROM UploadQuota WHERE Id = 1`, uploadquota.MaxLedgerBytes).Scan(&length, &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return uploadquota.Ledger{}, uploadquota.ErrSchema
	}
	if err != nil {
		if strings.Contains(err.Error(), "no such table: UploadQuota") {
			return uploadquota.Ledger{}, errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound, err)
		}
		if strings.Contains(err.Error(), "no such column:") {
			return uploadquota.Ledger{}, errors.Join(uploadquota.ErrSchema, err)
		}
		return uploadquota.Ledger{}, err
	}
	if length <= 0 || length > uploadquota.MaxLedgerBytes {
		return uploadquota.Ledger{}, uploadquota.ErrInvalid
	}
	return uploadquota.Decode(encoded)
}

func writeUploadQuota(ctx context.Context, conn *sql.Conn, encoded []byte) error {
	result, err := conn.ExecContext(ctx, "UPDATE UploadQuota SET State = ? WHERE Id = 1", encoded)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return uploadquota.ErrSchema
	}
	return nil
}

// quotaMetadata reads exactly the logical fields needed for accounting, through
// the same connection holding BEGIN IMMEDIATE and the reservation state.
func quotaMetadata(ctx context.Context, conn *sql.Conn) (map[string]models.File, error) {
	rows, err := conn.QueryContext(ctx, "SELECT Id, UserId, SizeBytes, UploadRequestId FROM FileMetaData")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make(map[string]models.File)
	for rows.Next() {
		var file models.File
		if err := rows.Scan(&file.Id, &file.UserId, &file.SizeBytes, &file.UploadRequestId); err != nil {
			return nil, err
		}
		files[file.Id] = file
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return files, nil
}

func (p DatabaseProvider) withUploadQuotaTransaction(ctx context.Context, action func(*sql.Conn) error) (err error) {
	if ctx == nil {
		return uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.sqliteDb == nil {
		return uploadquota.ErrSchema
	}
	conn, err := p.sqliteDb.Conn(ctx)
	if err != nil {
		return err
	}
	transactionOpen := true
	defer func() {
		if transactionOpen {
			// Request cancellation must not prevent releasing the write lock.
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, rollbackErr := conn.ExecContext(cleanup, "ROLLBACK")
			cancel()
			if rollbackErr != nil {
				// A failed BEGIN/COMMIT may leave transaction state uncertain.
				// Never return such a connection to the pool for another caller.
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
				err = errors.Join(err, fmt.Errorf("rollback upload quota: %w", rollbackErr))
			}
		}
		if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, sql.ErrConnDone) {
			err = errors.Join(err, closeErr)
		}
	}()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	if err := action(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	transactionOpen = false
	return nil
}

// initUploadQuota is used only for fresh databases and the schema migration.
// Ordinary reads/writes never recreate missing quota state on an existing DB.
func (p DatabaseProvider) initUploadQuota() error {
	encoded, err := uploadquota.Encode(uploadquota.EmptyLedger())
	if err != nil {
		return err
	}
	_, err = p.sqliteDb.Exec(`CREATE TABLE IF NOT EXISTS UploadQuota (
		Id INTEGER NOT NULL PRIMARY KEY CHECK (Id = 1),
		State BLOB NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = p.sqliteDb.Exec("INSERT OR IGNORE INTO UploadQuota (Id, State) VALUES (1, ?)", encoded)
	return err
}
