package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

// Both authorized identities and locators must still match inside the quota
// transaction. Display fields are compared in SQL without reading large values.
func checkReplacementMetadata(ctx context.Context, conn *sql.Conn, change uploadquota.Change) error {
	if err := uploadquota.ValidateReplacement(change); err != nil {
		return err
	}
	for _, expected := range []models.File{*change.Target, *change.Source} {
		current, err := readDuplicateSource(ctx, conn, expected.Id)
		if err != nil {
			return err
		}
		if err := uploadquota.CheckDuplicateSource(expected, current, change.Now); err != nil {
			return err
		}
	}
	var matches bool
	source := change.Source
	err := conn.QueryRowContext(ctx, `SELECT typeof(Name) = 'text' AND Name COLLATE BINARY = ?
		AND typeof(Size) = 'text' AND Size COLLATE BINARY = ?
		AND typeof(ContentType) = 'text' AND ContentType COLLATE BINARY = ?
		FROM FileMetaData WHERE Id = ?`, source.Name, source.Size, source.ContentType, source.Id).Scan(&matches)
	if errors.Is(err, sql.ErrNoRows) {
		return uploadquota.ErrConflict
	}
	if err != nil {
		return err
	}
	if !matches {
		return uploadquota.ErrConflict
	}
	return nil
}

// Patch only content fields; current owner, policy, counters and hotlink stay.
// Source expiry shares the commit and never refunds its logical charge here.
func patchReplacementMetadata(ctx context.Context, conn *sql.Conn, change uploadquota.Change) error {
	source, target := change.Source, change.Target
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(source.Encryption); err != nil {
		return err
	}
	result, err := conn.ExecContext(ctx, `UPDATE FileMetaData
		SET Name = ?, Size = ?, SHA1 = ?, ContentType = ?, AwsBucket = ?, SizeBytes = ?, Encryption = ?
		WHERE Id = ? AND UserId = ? AND UploadRequestId = '' AND PendingDeletion = 0`,
		source.Name, source.Size, source.SHA1, source.ContentType, source.AwsBucket, source.SizeBytes, encoded.Bytes(), target.Id, target.UserId)
	if err := requireReplacementRow(result, err); err != nil {
		return err
	}
	if change.DeleteSource {
		result, err = conn.ExecContext(ctx, `UPDATE FileMetaData SET ExpireAt = 0, UnlimitedTime = 0
			WHERE Id = ? AND UserId = ? AND PendingDeletion = 0`, source.Id, source.UserId)
		if err := requireReplacementRow(result, err); err != nil {
			return err
		}
	}
	return nil
}

func requireReplacementRow(result sql.Result, err error) error {
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
