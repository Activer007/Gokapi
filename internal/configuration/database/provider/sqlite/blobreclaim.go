package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/Activer007/SupaShare/internal/blobreclaim"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

type blobReclaimQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ReadBlobCandidates returns every bounded candidate or an error; never a
// truncated list that could hide an old content anchor.
func (p DatabaseProvider) ReadBlobCandidates(ctx context.Context) ([]blobreclaim.Candidate, error) {
	if ctx == nil {
		return nil, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.sqliteDb == nil {
		return nil, uploadquota.ErrSchema
	}
	return readBlobCandidates(ctx, p.sqliteDb)
}

func readBlobCandidates(ctx context.Context, query blobReclaimQuerier) ([]blobreclaim.Candidate, error) {
	rows, err := query.QueryContext(ctx, `SELECT
		CASE WHEN typeof(Id) = 'text' AND length(CAST(Id AS BLOB)) = 64 THEN Id ELSE NULL END,
		CASE WHEN typeof(State) = 'blob' AND length(State) BETWEEN 22 AND ? THEN State ELSE NULL END
		FROM BlobReclaims ORDER BY Id LIMIT ?`, blobreclaim.MaxRecordBytes, blobreclaim.MaxCandidates+1)
	if err != nil {
		return nil, blobReclaimReadError(ctx, err)
	}
	defer rows.Close()
	candidates := make([]blobreclaim.Candidate, 0)
	for rows.Next() {
		if len(candidates) == blobreclaim.MaxCandidates {
			return nil, uploadquota.ErrFull
		}
		var rawID any
		var encoded []byte
		if err := rows.Scan(&rawID, &encoded); err != nil {
			return nil, blobReclaimReadError(ctx, err)
		}
		id, ok := rawID.(string)
		if !ok || !validBlobCandidateKey(id) {
			return nil, uploadquota.ErrInvalid
		}
		candidate, err := blobreclaim.Decode(encoded)
		if err != nil {
			return nil, err
		}
		if candidate.Key() != id {
			return nil, uploadquota.ErrInvalid
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, blobReclaimReadError(ctx, err)
	}
	if err := rows.Close(); err != nil {
		return nil, blobReclaimReadError(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

// ReadBlobCandidate is the fresh token check used after acquiring a blob gate.
func (p DatabaseProvider) ReadBlobCandidate(ctx context.Context, key string) (blobreclaim.Candidate, bool, error) {
	if ctx == nil || !validBlobCandidateKey(key) {
		return blobreclaim.Candidate{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return blobreclaim.Candidate{}, false, err
	}
	if p.sqliteDb == nil {
		return blobreclaim.Candidate{}, false, uploadquota.ErrSchema
	}
	return readBlobCandidate(ctx, p.sqliteDb, key)
}

func readBlobCandidate(ctx context.Context, query quotaRowQuerier, key string) (blobreclaim.Candidate, bool, error) {
	var encoded []byte
	err := query.QueryRowContext(ctx, `SELECT
		CASE WHEN typeof(State) = 'blob' AND length(State) BETWEEN 22 AND ? THEN State ELSE NULL END
		FROM BlobReclaims WHERE Id = ?`, blobreclaim.MaxRecordBytes, key).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return blobreclaim.Candidate{}, false, nil
	}
	if err != nil {
		return blobreclaim.Candidate{}, false, blobReclaimReadError(ctx, err)
	}
	candidate, err := blobreclaim.Decode(encoded)
	if err != nil {
		return blobreclaim.Candidate{}, false, err
	}
	if candidate.Key() != key {
		return blobreclaim.Candidate{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return blobreclaim.Candidate{}, false, err
	}
	return candidate, true, nil
}

// RetireBlobCandidate acknowledges only the exact current intent. A new token
// for the same locator must survive an old worker's acknowledgement.
func (p DatabaseProvider) RetireBlobCandidate(ctx context.Context, expected blobreclaim.Candidate) (bool, error) {
	encoded, err := blobreclaim.Encode(expected)
	if err != nil {
		return false, err
	}
	retired := false
	err = p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		current, found, err := readBlobCandidate(ctx, conn, expected.Key())
		if err != nil || !found {
			return err
		}
		currentEncoded, err := blobreclaim.Encode(current)
		if err != nil {
			return err
		}
		if !bytes.Equal(currentEncoded, encoded) {
			return uploadquota.ErrConflict
		}
		result, err := conn.ExecContext(ctx, "DELETE FROM BlobReclaims WHERE Id = ? AND State = ?", expected.Key(), encoded)
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
		retired = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return retired, nil
}

// HasAnyBlobReference includes the original target ID and every owner. A
// replacement candidate is detached from a share identity, not from a locator.
func (p DatabaseProvider) HasAnyBlobReference(ctx context.Context, candidate blobreclaim.Candidate) (bool, error) {
	if ctx == nil {
		return false, uploadquota.ErrInvalid
	}
	if err := candidate.Validate(); err != nil {
		return false, err
	}
	return p.hasBlobReference(ctx, models.File{SHA1: candidate.SHA1, AwsBucket: candidate.AwsBucket}, "")
}

// ImportBlobCandidates is an offline copy into an empty collection. Validate
// all inputs before opening a transaction and never merge or reset a target.
func (p DatabaseProvider) ImportBlobCandidates(ctx context.Context, candidates []blobreclaim.Candidate) error {
	if ctx == nil {
		return uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(candidates) > blobreclaim.MaxCandidates {
		return uploadquota.ErrFull
	}
	encoded := make(map[string][]byte, len(candidates))
	for _, candidate := range candidates {
		value, err := blobreclaim.Encode(candidate)
		if err != nil {
			return err
		}
		key := candidate.Key()
		if _, exists := encoded[key]; exists {
			return uploadquota.ErrInvalid
		}
		encoded[key] = value
	}
	return p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		current, err := readBlobCandidates(ctx, conn)
		if err != nil {
			return err
		}
		if len(current) != 0 {
			return uploadquota.ErrConflict
		}
		for key, value := range encoded {
			if _, err := conn.ExecContext(ctx, "INSERT INTO BlobReclaims (Id, State) VALUES (?, ?)", key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// Called only after both authorized snapshots and logical capacity have been
// checked, on the same BEGIN IMMEDIATE connection as the content-field patch.
func registerReplacementBlob(ctx context.Context, conn *sql.Conn, change uploadquota.Change) error {
	target, source := change.Target, change.Source
	if target.SHA1 == source.SHA1 && target.AwsBucket == source.AwsBucket {
		return nil
	}
	candidate, err := blobreclaim.New(target.SHA1, target.AwsBucket)
	if err != nil {
		return err
	}
	current, err := readBlobCandidates(ctx, conn)
	if err != nil {
		return err
	}
	key, exists := candidate.Key(), false
	for _, previous := range current {
		if previous.Key() == key {
			if previous.SHA1 != candidate.SHA1 || previous.AwsBucket != candidate.AwsBucket {
				return uploadquota.ErrConflict
			}
			exists = true
		}
	}
	if !exists && len(current) == blobreclaim.MaxCandidates {
		return uploadquota.ErrFull
	}
	encoded, err := blobreclaim.Encode(candidate)
	if err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO BlobReclaims (Id, State) VALUES (?, ?)
		ON CONFLICT(Id) DO UPDATE SET State = excluded.State`, key, encoded)
	return err
}

func validBlobCandidateKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	for _, ch := range key {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

func blobReclaimReadError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if strings.Contains(err.Error(), "no such table: BlobReclaims") {
		return errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound)
	}
	if strings.Contains(err.Error(), "no such column:") {
		return uploadquota.ErrSchema
	}
	return err
}

// Schema setup is restricted to fresh database creation and explicit upgrade.
func (p DatabaseProvider) initBlobReclaims() error {
	if p.sqliteDb == nil {
		return uploadquota.ErrSchema
	}
	_, err := p.sqliteDb.Exec(`CREATE TABLE IF NOT EXISTS BlobReclaims (
		Id TEXT NOT NULL PRIMARY KEY,
		State BLOB NOT NULL
	)`)
	return err
}
