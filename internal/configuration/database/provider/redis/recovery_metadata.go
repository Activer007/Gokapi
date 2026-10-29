package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"regexp"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

var recoveryFileID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Select only fixed bounded fields atomically; unknown hash fields are omitted.
// Lua performs no integer conversion or recovery mutation.
var readRecoveryMetadata = redigo.NewScript(1, `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind == 'none' then return false end
if kind ~= 'hash' then return redis.error_reply('invalid recovery metadata type') end
if redis.call('HSTRLEN', KEYS[1], 'SHA1') == 0 or redis.call('HEXISTS', KEYS[1], 'AwsBucket') ~= 1 then
    return redis.error_reply('invalid recovery metadata locator')
end
for _, field in ipairs({'ExpireAt','PendingDeletion','SizeBytes','UploadDate','DownloadsRemaining','DownloadCount','UserId','UnlimitedDownloads','UnlimitedTime'}) do
    local length = redis.call('HSTRLEN', KEYS[1], field)
    if length == 0 or length > 20 then return redis.error_reply('invalid recovery metadata integer') end
end
for _, field in ipairs({'UnlimitedDownloads','UnlimitedTime'}) do
    local value = redis.call('HGET', KEYS[1], field)
    if value ~= '0' and value ~= '1' then return redis.error_reply('invalid recovery metadata flag') end
end
local fields = {
    {'Name',65536},{'Size',65536},{'SHA1',128},{'PasswordHash',65536},{'HotlinkId',65536},
    {'ContentType',65536},{'AwsBucket',1024},{'FileRequestId',128},{'EncryptionRedis',4096},
    {'ExpireAt',20},{'PendingDeletion',20},{'SizeBytes',20},{'UploadDate',20},
    {'DownloadsRemaining',20},{'DownloadCount',20},{'UserId',20},{'UnlimitedDownloads',20},{'UnlimitedTime',20}
}
local result = {}
for _, field in ipairs(fields) do
    if redis.call('HSTRLEN', KEYS[1], field[1]) > field[2] then
        return redis.error_reply('invalid recovery metadata size')
    end
    local value = redis.call('HGET', KEYS[1], field[1])
    if value then table.insert(result, field[1]); table.insert(result, value) end
end
return result
`)

// ReadRecoveryFile is a context-bound full model read without panic or logging.
func (p DatabaseProvider) ReadRecoveryFile(ctx context.Context, id string) (models.File, bool, error) {
	if ctx == nil || !recoveryFileID.MatchString(id) {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, false, err
	}
	if p.pool == nil {
		return models.File{}, false, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return models.File{}, false, err
	}
	defer conn.Close()
	return p.readRecoveryFileConn(ctx, conn, id)
}

// The caller may already hold WATCH on this key for a checked mutation.
func (p DatabaseProvider) readRecoveryFileConn(ctx context.Context, conn redigo.Conn, id string) (models.File, bool, error) {
	if ctx == nil || conn == nil || !recoveryFileID.MatchString(id) {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, false, err
	}
	values, err := redigo.Values(readRecoveryMetadata.DoContext(ctx, conn, p.dbPrefix+prefixMetaData+id))
	if errors.Is(err, redigo.ErrNil) {
		return models.File{}, false, nil
	}
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return models.File{}, false, contextErr
		}
		var redisErr redigo.Error
		if errors.As(err, &redisErr) {
			return models.File{}, false, uploadquota.ErrInvalid
		}
		return models.File{}, false, err
	}
	if len(values) == 0 || len(values)%2 != 0 {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	file, err := dbToMetadata(id, values)
	if err != nil {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if file.UserId <= 0 || file.SizeBytes < 0 || file.PendingDeletion < 0 || file.DownloadCount < 0 {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	for i := 0; i < len(values); i += 2 {
		field, _ := redigo.String(values[i], nil)
		if field != "EncryptionRedis" {
			continue
		}
		data, err := redigo.Bytes(values[i+1], nil)
		if err != nil {
			return models.File{}, false, uploadquota.ErrInvalid
		}
		reader := bytes.NewReader(data)
		var checked models.EncryptionInfo
		if err := gob.NewDecoder(reader).Decode(&checked); err != nil || reader.Len() != 0 || len(checked.DecryptionKey) > 128 || len(checked.Nonce) > 128 {
			return models.File{}, false, uploadquota.ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, false, err
	}
	return file, true, nil
}
