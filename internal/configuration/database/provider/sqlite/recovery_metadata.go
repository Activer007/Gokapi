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
	// All identifiers are fixed schema columns, never caller-provided SQL.
	columns := []string{"Id", "Name", "Size", "SHA1", "ExpireAt", "SizeBytes", "DownloadsRemaining", "DownloadCount", "PasswordHash", "HotlinkId", "ContentType", "AwsBucket", "Encryption", "UnlimitedDownloads", "UnlimitedTime", "UserId", "UploadDate", "PendingDeletion", "UploadRequestId"}
	limits := []int{128, 64 << 10, 64 << 10, 128, 20, 20, 20, 20, 64 << 10, 64 << 10, 64 << 10, 1024, 4096, 20, 20, 20, 20, 20, 128}
	for i, column := range columns {
		columns[i] = "CASE WHEN length(CAST(" + column + " AS BLOB)) <= " + strconv.Itoa(limits[i]) + " THEN " + column + " ELSE NULL END"
	}
	var row schemaMetaData
	err := p.sqliteDb.QueryRowContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM FileMetaData WHERE Id = ?", id).
		Scan(&row.Id, &row.Name, &row.Size, &row.SHA1, &row.ExpireAt, &row.SizeBytes, &row.DownloadsRemaining, &row.DownloadCount,
			&row.PasswordHash, &row.HotlinkId, &row.ContentType, &row.AwsBucket, &row.Encryption, &row.UnlimitedDownloads,
			&row.UnlimitedTime, &row.UserId, &row.UploadDate, &row.PendingDeletion, &row.UploadRequestId)
	if errors.Is(err, sql.ErrNoRows) {
		return models.File{}, false, nil
	}
	if err != nil {
		return models.File{}, false, err
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
