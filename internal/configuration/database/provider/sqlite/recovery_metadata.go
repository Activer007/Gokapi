package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

var recoveryFileID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// ReadRecoveryFile preserves the full legacy model for receipt digest checks,
// while bounding every SQL value before it crosses into Go. It never logs.
func (p DatabaseProvider) ReadRecoveryFile(ctx context.Context, id string) (models.File, bool, error) {
	if ctx == nil || !recoveryFileID.MatchString(id) {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, false, err
	}
	if p.sqliteDb == nil {
		return models.File{}, false, uploadquota.ErrSchema
	}
	return readRecoveryFile(ctx, p.sqliteDb, id)
}

type recoveryFileQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// The caller validates context and ID; a transaction can reuse the same bounded
// full read without opening a second database connection.
func readRecoveryFile(ctx context.Context, queryer recoveryFileQueryer, id string) (models.File, bool, error) {
	// All identifiers are fixed schema columns, never caller-provided SQL.
	columns := []string{"Id", "Name", "Size", "SHA1", "ExpireAt", "SizeBytes", "DownloadsRemaining", "DownloadCount", "PasswordHash", "HotlinkId", "ContentType", "AwsBucket", "Encryption", "UnlimitedDownloads", "UnlimitedTime", "UserId", "UploadDate", "PendingDeletion", "UploadRequestId"}
	limits := []int{128, 64 << 10, 64 << 10, 128, 20, 20, 20, 20, 64 << 10, 64 << 10, 64 << 10, 1024, 4096, 20, 20, 20, 20, 20, 128}
	for i, column := range columns {
		kind := "text"
		switch column {
		case "ExpireAt", "SizeBytes", "DownloadsRemaining", "DownloadCount", "UnlimitedDownloads", "UnlimitedTime", "UserId", "UploadDate", "PendingDeletion":
			kind = "integer"
		case "Encryption":
			kind = "blob"
		}
		columns[i] = "CASE WHEN typeof(" + column + ") = '" + kind + "' AND length(CAST(" + column + " AS BLOB)) <= " + strconv.Itoa(limits[i]) + " THEN " + column + " ELSE NULL END"
	}
	var row schemaMetaData
	err := queryer.QueryRowContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM FileMetaData WHERE Id = ?", id).
		Scan(&row.Id, &row.Name, &row.Size, &row.SHA1, &row.ExpireAt, &row.SizeBytes, &row.DownloadsRemaining, &row.DownloadCount,
			&row.PasswordHash, &row.HotlinkId, &row.ContentType, &row.AwsBucket, &row.Encryption, &row.UnlimitedDownloads,
			&row.UnlimitedTime, &row.UserId, &row.UploadDate, &row.PendingDeletion, &row.UploadRequestId)
	if errors.Is(err, sql.ErrNoRows) {
		return models.File{}, false, nil
	}
	if err != nil {
		return models.File{}, false, err
	}
	if row.Id != id || row.UserId <= 0 || row.SizeBytes < 0 || row.DownloadCount < 0 || row.PendingDeletion < 0 ||
		(row.UnlimitedDownloads != 0 && row.UnlimitedDownloads != 1) || (row.UnlimitedTime != 0 && row.UnlimitedTime != 1) {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	file, err := row.ToFileModel()
	if err != nil {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	reader := bytes.NewReader(row.Encryption)
	var checked models.EncryptionInfo
	if err := gob.NewDecoder(reader).Decode(&checked); err != nil || reader.Len() != 0 || len(checked.DecryptionKey) > 128 || len(checked.Nonce) > 128 {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, false, err
	}
	return file, true, nil
}
