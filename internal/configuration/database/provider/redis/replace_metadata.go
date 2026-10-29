package redis

import (
	"context"
	"errors"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// Compare bounded text in the same atomic read that verifies each length.
// Passwords and target policy fields are neither read nor copied by this step.
var readCurrentReplacementText = redigo.NewScript(1, `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind == 'none' then return {} end
if kind ~= 'hash' then return redis.error_reply('invalid replacement source type') end
local fields = {'Name', 'Size', 'ContentType'}
local result = {}
for i, field in ipairs(fields) do
    if redis.call('HSTRLEN', KEYS[1], field) > 65536 then
        return redis.error_reply('invalid replacement text size')
    end
    result[i] = redis.call('HGET', KEYS[1], field) or ''
end
return result
`)

func (p DatabaseProvider) prepareReplacement(ctx context.Context, conn redigo.Conn, change uploadquota.Change) (redigo.Args, error) {
	if err := uploadquota.ValidateReplacement(change); err != nil {
		return nil, err
	}
	for _, expected := range []models.File{*change.Target, *change.Source} {
		current, err := p.readDuplicateSource(ctx, conn, expected.Id)
		if err != nil {
			return nil, err
		}
		if err := uploadquota.CheckDuplicateSource(expected, current, change.Now); err != nil {
			return nil, err
		}
	}
	values, err := redigo.Strings(readCurrentReplacementText.DoContext(ctx, conn, p.dbPrefix+prefixMetaData+change.Source.Id))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		var redisErr redigo.Error
		if errors.As(err, &redisErr) {
			return nil, uploadquota.ErrInvalid
		}
		return nil, err
	}
	if len(values) == 0 {
		return nil, uploadquota.ErrConflict
	}
	if len(values) != 3 {
		return nil, uploadquota.ErrInvalid
	}
	for i, expected := range []string{change.Source.Name, change.Source.Size, change.Source.ContentType} {
		if len(values[i]) > uploadquota.MaxMetadataTextBytes {
			return nil, uploadquota.ErrInvalid
		}
		if values[i] != expected {
			return nil, uploadquota.ErrConflict
		}
	}
	source, err := marshalEncryptionInfo(*change.Source)
	if err != nil {
		return nil, err
	}
	if len(source.InternalRedisEncryption) > 4096 {
		return nil, uploadquota.ErrInvalid
	}
	// HSET only these seven content fields. Never overwrite current owner,
	// password, expiry, download counters, request attribution or hotlink.
	return p.buildArgs(prefixMetaData+change.Target.Id).Add(
		"Name", source.Name, "Size", source.Size, "SHA1", source.SHA1,
		"ContentType", source.ContentType, "AwsBucket", source.AwsBucket,
		"SizeBytes", source.SizeBytes, "EncryptionRedis", source.InternalRedisEncryption), nil
}
