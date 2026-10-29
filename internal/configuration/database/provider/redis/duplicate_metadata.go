package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"strconv"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// Lua only selects bounded fields atomically. It never converts quota integers
// to numbers or reads display names, passwords or other unrelated metadata.
var readCurrentDuplicateSource = redigo.NewScript(1, `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind == 'none' then return {} end
if kind ~= 'hash' then return redis.error_reply('invalid duplicate source type') end
local numeric = {'UserId', 'SizeBytes', 'PendingDeletion', 'ExpireAt', 'UnlimitedTime', 'DownloadsRemaining', 'UnlimitedDownloads'}
for _, field in ipairs(numeric) do
    local length = redis.call('HSTRLEN', KEYS[1], field)
    if length == 0 or length > 20 then return redis.error_reply('invalid duplicate source integer') end
end
local bounded = {{'FileRequestId', 128}, {'SHA1', 128}, {'AwsBucket', 1024}, {'EncryptionRedis', 4096}}
for _, field in ipairs(bounded) do
    if redis.call('HSTRLEN', KEYS[1], field[1]) > field[2] then
        return redis.error_reply('invalid duplicate source field size')
    end
end
return {
    redis.call('HGET', KEYS[1], 'UserId'),
    redis.call('HGET', KEYS[1], 'FileRequestId') or '',
    redis.call('HGET', KEYS[1], 'SizeBytes'),
    redis.call('HGET', KEYS[1], 'SHA1') or '',
    redis.call('HGET', KEYS[1], 'AwsBucket') or '',
    redis.call('HGET', KEYS[1], 'EncryptionRedis') or '',
    redis.call('HGET', KEYS[1], 'PendingDeletion'),
    redis.call('HGET', KEYS[1], 'ExpireAt'),
    redis.call('HGET', KEYS[1], 'UnlimitedTime'),
    redis.call('HGET', KEYS[1], 'DownloadsRemaining'),
    redis.call('HGET', KEYS[1], 'UnlimitedDownloads')
}
`)

func (p DatabaseProvider) readDuplicateSource(ctx context.Context, conn redigo.Conn, id string) (models.File, error) {
	if ctx == nil || id == "" || len(id) > uploadquota.MaxIDLength {
		return models.File{}, uploadquota.ErrInvalid
	}
	key := p.dbPrefix + prefixMetaData + id
	// Download counters and external writers need not change the global metadata
	// revision. Watch the source itself before reading its current live state.
	if _, err := redigo.DoContext(conn, ctx, "WATCH", key); err != nil {
		return models.File{}, err
	}
	values, err := redigo.Values(readCurrentDuplicateSource.DoContext(ctx, conn, key))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return models.File{}, contextErr
		}
		var redisErr redigo.Error
		if errors.As(err, &redisErr) {
			return models.File{}, uploadquota.ErrInvalid
		}
		return models.File{}, err
	}
	if len(values) == 0 {
		return models.File{}, uploadquota.ErrConflict
	}
	if len(values) != 11 {
		return models.File{}, uploadquota.ErrInvalid
	}
	var numbers [7]int64
	for i, position := range [...]int{0, 2, 6, 7, 8, 9, 10} {
		value, err := redigo.String(values[position], nil)
		if err != nil || len(value) == 0 || len(value) > 20 {
			return models.File{}, uploadquota.ErrInvalid
		}
		numbers[i], err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return models.File{}, uploadquota.ErrInvalid
		}
	}
	if int64(int(numbers[0])) != numbers[0] || int64(int(numbers[5])) != numbers[5] ||
		(numbers[4] != 0 && numbers[4] != 1) || (numbers[6] != 0 && numbers[6] != 1) {
		return models.File{}, uploadquota.ErrInvalid
	}
	requestID, err := redigo.String(values[1], nil)
	if err != nil || len(requestID) > uploadquota.MaxIDLength {
		return models.File{}, uploadquota.ErrInvalid
	}
	hash, err := redigo.String(values[3], nil)
	if err != nil || len(hash) > 128 {
		return models.File{}, uploadquota.ErrInvalid
	}
	bucket, err := redigo.String(values[4], nil)
	if err != nil || len(bucket) > 1024 {
		return models.File{}, uploadquota.ErrInvalid
	}
	data, err := redigo.Bytes(values[5], nil)
	if err != nil || len(data) > 4096 {
		return models.File{}, uploadquota.ErrInvalid
	}
	var encryption models.EncryptionInfo
	if len(data) != 0 {
		reader := bytes.NewReader(data)
		if err := gob.NewDecoder(reader).Decode(&encryption); err != nil || reader.Len() != 0 {
			return models.File{}, uploadquota.ErrInvalid
		}
	}
	if len(encryption.DecryptionKey) > 128 || len(encryption.Nonce) > 128 {
		return models.File{}, uploadquota.ErrInvalid
	}
	return models.File{
		Id:                 id,
		UserId:             int(numbers[0]),
		UploadRequestId:    requestID,
		SizeBytes:          numbers[1],
		SHA1:               hash,
		AwsBucket:          bucket,
		Encryption:         encryption,
		PendingDeletion:    numbers[2],
		ExpireAt:           numbers[3],
		UnlimitedTime:      numbers[4] == 1,
		DownloadsRemaining: int(numbers[5]),
		UnlimitedDownloads: numbers[6] == 1,
	}, nil
}
