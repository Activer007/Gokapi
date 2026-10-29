package sqlite

import (
	"bytes"
	"context"
	"encoding/gob"
	"reflect"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// ReadBlobEncryption is a checked projection used while publication holds the
// storage locator lock. Unrelated large names/passwords never cross this query.
func (p DatabaseProvider) ReadBlobEncryption(ctx context.Context, hash, bucket string) (models.EncryptionInfo, bool, error) {
	if ctx == nil || hash == "" || len(hash) > uploadquota.MaxIDLength || len(bucket) > 1024 {
		return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.EncryptionInfo{}, false, err
	}
	if p.sqliteDb == nil {
		return models.EncryptionInfo{}, false, uploadquota.ErrSchema
	}
	rows, err := p.sqliteDb.QueryContext(ctx, `SELECT length(CAST(Encryption AS BLOB)),
		CASE WHEN length(CAST(Encryption AS BLOB)) <= 4096 THEN CAST(Encryption AS BLOB) ELSE NULL END
		FROM FileMetaData WHERE SHA1 = ? AND AwsBucket = ? LIMIT 65537`, hash, bucket)
	if err != nil {
		return models.EncryptionInfo{}, false, err
	}
	defer rows.Close()
	var result models.EncryptionInfo
	found := false
	count := 0
	for rows.Next() {
		count++
		if count > 65536 {
			return models.EncryptionInfo{}, false, uploadquota.ErrFull
		}
		var size int64
		var data []byte
		if err := rows.Scan(&size, &data); err != nil {
			return models.EncryptionInfo{}, false, err
		}
		if size <= 0 || size > 4096 || int64(len(data)) != size {
			return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
		}
		var info models.EncryptionInfo
		reader := bytes.NewReader(data)
		decoder := gob.NewDecoder(reader)
		if err := decoder.Decode(&info); err != nil {
			return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
		}
		if reader.Len() != 0 || len(info.DecryptionKey) > 128 || len(info.Nonce) > 128 {
			return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
		}
		if found && !reflect.DeepEqual(info, result) {
			return models.EncryptionInfo{}, false, uploadquota.ErrConflict
		}
		result, found = info, true
	}
	if err := rows.Err(); err != nil {
		return models.EncryptionInfo{}, false, err
	}
	return result, found, nil
}
