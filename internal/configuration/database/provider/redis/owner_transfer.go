package redis

import (
	"context"
	"errors"
	"strconv"

	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// Transfer reads only bounded identity/accounting fields. Lua does not convert
// integers, read encryption material or load either user's password/name.
var readCurrentOwnerTransfer = redigo.NewScript(2, `
local sourceType = redis.call('TYPE', KEYS[1]).ok
local targetType = redis.call('TYPE', KEYS[2]).ok
if sourceType == 'none' or targetType == 'none' then return {} end
if sourceType ~= 'hash' or targetType ~= 'hash' then
    return redis.error_reply('invalid owner transfer type')
end
for _, field in ipairs({'UserId', 'SizeBytes', 'PendingDeletion'}) do
    local length = redis.call('HSTRLEN', KEYS[1], field)
    if length == 0 or length > 20 then return redis.error_reply('invalid owner transfer integer') end
end
local targetLength = redis.call('HSTRLEN', KEYS[2], 'id')
if targetLength == 0 or targetLength > 20 or redis.call('HSTRLEN', KEYS[1], 'FileRequestId') > 128 then
    return redis.error_reply('invalid owner transfer identity')
end
return {
    redis.call('HGET', KEYS[1], 'UserId'),
    redis.call('HGET', KEYS[1], 'SizeBytes'),
    redis.call('HGET', KEYS[1], 'FileRequestId') or '',
    redis.call('HGET', KEYS[1], 'PendingDeletion'),
    redis.call('HGET', KEYS[2], 'id')
}
`)

func (p DatabaseProvider) checkOwnerTransfer(ctx context.Context, conn redigo.Conn, change uploadquota.Change) error {
	if change.Action != uploadquota.TransferOwner || change.Source == nil || change.Source.Id == "" ||
		len(change.Source.Id) > uploadquota.MaxIDLength || change.Source.Id != change.Upload.ID || change.Upload.UserID <= 0 {
		return uploadquota.ErrInvalid
	}
	sourceKey := p.dbPrefix + prefixMetaData + change.Source.Id
	targetKey := p.dbPrefix + prefixUsers + strconv.Itoa(change.Upload.UserID)
	if _, err := redigo.DoContext(conn, ctx, "WATCH", sourceKey, targetKey); err != nil {
		return err
	}
	values, err := redigo.Strings(readCurrentOwnerTransfer.DoContext(ctx, conn, sourceKey, targetKey))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		var redisErr redigo.Error
		if errors.As(err, &redisErr) {
			return uploadquota.ErrInvalid
		}
		return err
	}
	if len(values) == 0 {
		return uploadquota.ErrConflict
	}
	if len(values) != 5 || len(values[2]) > uploadquota.MaxIDLength {
		return uploadquota.ErrInvalid
	}
	var numbers [4]int64
	for i, position := range [...]int{0, 1, 3, 4} {
		if len(values[position]) == 0 || len(values[position]) > 20 {
			return uploadquota.ErrInvalid
		}
		numbers[i], err = strconv.ParseInt(values[position], 10, 64)
		if err != nil {
			return uploadquota.ErrInvalid
		}
	}
	if numbers[0] != int64(change.Source.UserId) || numbers[1] != change.Source.SizeBytes ||
		values[2] != change.Source.UploadRequestId || numbers[2] != 0 || numbers[3] != int64(change.Upload.UserID) {
		return uploadquota.ErrConflict
	}
	return nil
}
