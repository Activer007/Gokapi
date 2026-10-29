package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/gob"
	"errors"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// readDuplicateSource checks only the source identity, blob and live state on
// the connection holding BEGIN IMMEDIATE. Large display fields are omitted.
func readDuplicateSource(ctx context.Context, conn *sql.Conn, id string) (models.File, error) {
	if id == "" || len(id) > uploadquota.MaxIDLength {
		return models.File{}, uploadquota.ErrInvalid
	}
	var numbers [7]any
	var strings [3]any
	var encoded []byte
	err := conn.QueryRowContext(ctx, `SELECT UserId, SizeBytes, PendingDeletion, ExpireAt,
		UnlimitedTime, DownloadsRemaining, UnlimitedDownloads,
		CASE WHEN typeof(SHA1) = 'text' AND length(CAST(SHA1 AS BLOB)) <= 128 THEN SHA1 ELSE NULL END,
		CASE WHEN typeof(AwsBucket) = 'text' AND length(CAST(AwsBucket AS BLOB)) <= 1024 THEN AwsBucket ELSE NULL END,
		CASE WHEN typeof(UploadRequestId) = 'text' AND length(CAST(UploadRequestId AS BLOB)) <= 128 THEN UploadRequestId ELSE NULL END,
		CASE WHEN typeof(Encryption) = 'blob' AND length(Encryption) BETWEEN 1 AND 4096 THEN Encryption ELSE NULL END
		FROM FileMetaData WHERE Id = ?`, id).
		Scan(&numbers[0], &numbers[1], &numbers[2], &numbers[3], &numbers[4], &numbers[5], &numbers[6],
			&strings[0], &strings[1], &strings[2], &encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return models.File{}, uploadquota.ErrConflict
	}
	if err != nil {
		return models.File{}, err
	}
	var integers [7]int64
	for i, value := range numbers {
		integer, ok := value.(int64)
		if !ok {
			return models.File{}, uploadquota.ErrInvalid
		}
		integers[i] = integer
	}
	var texts [3]string
	for i, value := range strings {
		str, ok := value.(string)
		if !ok {
			return models.File{}, uploadquota.ErrInvalid
		}
		texts[i] = str
	}
	if integers[0] <= 0 || int64(int(integers[0])) != integers[0] || int64(int(integers[5])) != integers[5] ||
		(integers[4] != 0 && integers[4] != 1) || (integers[6] != 0 && integers[6] != 1) ||
		len(encoded) == 0 || len(encoded) > 4096 {
		return models.File{}, uploadquota.ErrInvalid
	}
	var encryption models.EncryptionInfo
	reader := bytes.NewReader(encoded)
	if err := gob.NewDecoder(reader).Decode(&encryption); err != nil {
		return models.File{}, uploadquota.ErrInvalid
	}
	if reader.Len() != 0 || len(encryption.DecryptionKey) > 128 || len(encryption.Nonce) > 128 {
		return models.File{}, uploadquota.ErrInvalid
	}
	return models.File{
		Id: id, UserId: int(integers[0]), SizeBytes: integers[1], PendingDeletion: integers[2],
		ExpireAt: integers[3], UnlimitedTime: integers[4] == 1, DownloadsRemaining: int(integers[5]),
		UnlimitedDownloads: integers[6] == 1, SHA1: texts[0], AwsBucket: texts[1],
		UploadRequestId: texts[2], Encryption: encryption,
	}, nil
}
