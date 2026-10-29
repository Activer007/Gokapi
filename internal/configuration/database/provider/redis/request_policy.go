package redis

import (
	"context"
	"errors"
	"strconv"

	"github.com/forceu/gokapi/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// Lua performs only an atomic, bounded projection. Integer parsing and policy
// arithmetic stay in Go, avoiding Redis/Lua floating-point conversions.
var readCurrentRequestPolicy = redigo.NewScript(1, `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind == 'none' then return {} end
if kind ~= 'hash' then return redis.error_reply('invalid request policy type') end
local fields = {'userid', 'maxfiles', 'maxsize', 'expiry'}
local result = {}
for i, field in ipairs(fields) do
    local length = redis.call('HSTRLEN', KEYS[1], field)
    if length == 0 or length > 20 then return redis.error_reply('invalid request policy integer') end
    result[i] = redis.call('HGET', KEYS[1], field)
end
return result
`)

func (p DatabaseProvider) readRequestPolicy(ctx context.Context, conn redigo.Conn, requestID string) (uploadquota.RequestPolicy, error) {
	if len(requestID) == 0 || len(requestID) > uploadquota.MaxIDLength {
		return uploadquota.RequestPolicy{}, uploadquota.ErrInvalid
	}
	key := p.dbPrefix + prefixFileRequests + requestID
	if _, err := redigo.DoContext(conn, ctx, "WATCH", key); err != nil {
		return uploadquota.RequestPolicy{}, err
	}
	values, err := redigo.Strings(readCurrentRequestPolicy.DoContext(ctx, conn, key))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return uploadquota.RequestPolicy{}, contextErr
		}
		var redisErr redigo.Error
		if errors.As(err, &redisErr) {
			return uploadquota.RequestPolicy{}, uploadquota.ErrInvalid
		}
		return uploadquota.RequestPolicy{}, err
	}
	if len(values) == 0 {
		return uploadquota.RequestPolicy{}, uploadquota.ErrNotFound
	}
	if len(values) != 4 {
		return uploadquota.RequestPolicy{}, uploadquota.ErrInvalid
	}
	var parsed [4]int64
	for i, value := range values {
		if len(value) == 0 || len(value) > 20 {
			return uploadquota.RequestPolicy{}, uploadquota.ErrInvalid
		}
		parsed[i], err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return uploadquota.RequestPolicy{}, uploadquota.ErrInvalid
		}
	}
	return uploadquota.RequestPolicy{UserID: parsed[0], MaxFiles: parsed[1], MaxSizeMB: parsed[2], Expiry: parsed[3]}, nil
}
