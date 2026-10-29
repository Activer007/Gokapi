package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"reflect"
	"strings"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// Only bounded field selection is done in Lua; all quota arithmetic remains
// checked int64 Go code. Length checks and projection form one atomic read.
var readBlobEncryption = redigo.NewScript(1, `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind == 'none' then return {} end
if kind ~= 'hash' then return redis.error_reply('invalid metadata type') end
if redis.call('HSTRLEN', KEYS[1], 'SHA1') > 128 or redis.call('HSTRLEN', KEYS[1], 'AwsBucket') > 1024 then
    return redis.error_reply('invalid blob locator')
end
local hash = redis.call('HGET', KEYS[1], 'SHA1')
local bucket = redis.call('HGET', KEYS[1], 'AwsBucket') or ''
if hash ~= ARGV[1] or bucket ~= ARGV[2] then return {} end
if redis.call('HSTRLEN', KEYS[1], 'EncryptionRedis') > 4096 then
    return redis.error_reply('invalid encryption metadata size')
end
return {hash, bucket, redis.call('HGET', KEYS[1], 'EncryptionRedis') or ''}
`)

func (p DatabaseProvider) ReadBlobEncryption(ctx context.Context, hash, bucket string) (models.EncryptionInfo, bool, error) {
	if ctx == nil || hash == "" || len(hash) > uploadquota.MaxIDLength || len(bucket) > 1024 {
		return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.EncryptionInfo{}, false, err
	}
	if p.pool == nil {
		return models.EncryptionInfo{}, false, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return models.EncryptionInfo{}, false, err
	}
	defer conn.Close()
	prefix := p.dbPrefix + prefixMetaData
	pattern := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(prefix) + "*"
	cursor := "0"
	seen := make(map[string]struct{})
	var result models.EncryptionInfo
	found := false
	for batch := 0; batch < 4096; batch++ {
		reply, err := redigo.Values(redigo.DoContext(conn, ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", 128))
		if err != nil {
			return models.EncryptionInfo{}, false, err
		}
		if len(reply) != 2 {
			return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
		}
		cursor, err = redigo.String(reply[0], nil)
		if err != nil {
			return models.EncryptionInfo{}, false, err
		}
		keys, err := redigo.Strings(reply[1], nil)
		if err != nil || len(keys) > 1024 {
			return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, prefix) {
				return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			if len(seen) >= 65536 {
				return models.EncryptionInfo{}, false, uploadquota.ErrFull
			}
			seen[key] = struct{}{}
			values, err := redigo.Values(readBlobEncryption.DoContext(ctx, conn, key, hash, bucket))
			if err != nil {
				return models.EncryptionInfo{}, false, err
			}
			if len(values) == 0 {
				continue
			}
			if len(values) != 3 {
				return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
			}
			data, err := redigo.Bytes(values[2], nil)
			if err != nil || len(data) > 4096 {
				return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
			}
			var info models.EncryptionInfo
			if len(data) > 0 {
				reader := bytes.NewReader(data)
				decoder := gob.NewDecoder(reader)
				if err := decoder.Decode(&info); err != nil {
					return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
				}
				if reader.Len() != 0 {
					return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
				}
			}
			if len(info.DecryptionKey) > 128 || len(info.Nonce) > 128 {
				return models.EncryptionInfo{}, false, uploadquota.ErrInvalid
			}
			if found && !reflect.DeepEqual(info, result) {
				return models.EncryptionInfo{}, false, uploadquota.ErrConflict
			}
			result, found = info, true
		}
		if cursor == "0" {
			return result, found, nil
		}
	}
	return models.EncryptionInfo{}, false, uploadquota.ErrRetry
}
