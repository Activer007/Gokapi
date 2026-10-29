package redis

import (
	"context"
	"crypto/rand"
	"errors"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

const maxCleanupFiles = 16384

// Return a bounded page, including a checked cursor. No metadata fields are
// materialized while enumerating candidates.
var readCleanupPage = redigo.NewScript(0, `
local result = redis.call('SCAN', ARGV[1], 'MATCH', ARGV[2], 'COUNT', 128)
if #result[2] > 1024 then return redis.error_reply('invalid cleanup page') end
for _, key in ipairs(result[2]) do
    if #key > tonumber(ARGV[3]) then return redis.error_reply('invalid cleanup key') end
end
return result
`)

var readCleanupLocator = redigo.NewScript(1, `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind == 'none' then return {} end
if kind ~= 'hash' then return redis.error_reply('invalid cleanup metadata type') end
local length = redis.call('HSTRLEN', KEYS[1], 'SHA1')
if length == 0 or length > 128 or redis.call('HEXISTS', KEYS[1], 'AwsBucket') ~= 1 or redis.call('HSTRLEN', KEYS[1], 'AwsBucket') > 1024 then
    return redis.error_reply('invalid cleanup locator')
end
return {redis.call('HGET', KEYS[1], 'SHA1'), redis.call('HGET', KEYS[1], 'AwsBucket') or ''}
`)

func cleanupRedisError(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	var redisErr redigo.Error
	if errors.As(err, &redisErr) {
		return uploadquota.ErrInvalid
	}
	return err
}

// ReadCleanupIDs either finishes the bounded enumeration or returns no IDs.
func (p DatabaseProvider) ReadCleanupIDs(ctx context.Context) ([]string, error) {
	if ctx == nil {
		return nil, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.pool == nil {
		return nil, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return p.readCleanupIDsConn(ctx, conn)
}

func (p DatabaseProvider) readCleanupIDsConn(ctx context.Context, conn redigo.Conn) ([]string, error) {
	prefix := p.dbPrefix + prefixMetaData
	pattern := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(prefix) + "*"
	cursor := "0"
	seen := make(map[string]struct{})
	for batch := 0; batch < 4096; batch++ {
		reply, err := redigo.Values(readCleanupPage.DoContext(ctx, conn, cursor, pattern, len(prefix)+128))
		if err != nil {
			return nil, cleanupRedisError(ctx, err)
		}
		if len(reply) != 2 {
			return nil, uploadquota.ErrInvalid
		}
		cursor, err = redigo.String(reply[0], nil)
		if err != nil || len(cursor) == 0 || len(cursor) > 20 {
			return nil, uploadquota.ErrInvalid
		}
		keys, err := redigo.Strings(reply[1], nil)
		if err != nil || len(keys) > 1024 {
			return nil, uploadquota.ErrInvalid
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, prefix) {
				return nil, uploadquota.ErrInvalid
			}
			id := strings.TrimPrefix(key, prefix)
			if !recoveryFileID.MatchString(id) {
				return nil, uploadquota.ErrInvalid
			}
			seen[id] = struct{}{}
			if len(seen) > maxCleanupFiles {
				return nil, uploadquota.ErrFull
			}
		}
		if cursor == "0" {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			ids := make([]string, 0, len(seen))
			for id := range seen {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			return ids, nil
		}
	}
	return nil, uploadquota.ErrRetry
}

// HasBlobReference checks every locator before reporting a complete result.
// Display fields and encryption material of other owners are never decoded.
func (p DatabaseProvider) HasBlobReference(ctx context.Context, file models.File) (bool, error) {
	if ctx == nil || !recoveryFileID.MatchString(file.Id) || file.SHA1 == "" || len(file.SHA1) > 128 || len(file.AwsBucket) > 1024 {
		return false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.pool == nil {
		return false, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ids, err := p.readCleanupIDsConn(ctx, conn)
	if err != nil {
		return false, err
	}
	found := false
	for _, id := range ids {
		if id == file.Id {
			continue
		}
		values, err := redigo.Strings(readCleanupLocator.DoContext(ctx, conn, p.dbPrefix+prefixMetaData+id))
		if err != nil {
			return false, cleanupRedisError(ctx, err)
		}
		if len(values) == 0 {
			continue
		}
		if len(values) != 2 {
			return false, uploadquota.ErrInvalid
		}
		if values[0] == file.SHA1 && values[1] == file.AwsBucket {
			found = true
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return found, nil
}

// RetireCleanupFile performs no physical I/O. sourceGone is evidence supplied
// by storage after successful deletion or a checked missing-source result.
func (p DatabaseProvider) RetireCleanupFile(ctx context.Context, expected models.File, now int64, sourceGone bool) (bool, error) {
	if ctx == nil || !recoveryFileID.MatchString(expected.Id) || expected.UserId <= 0 || expected.SizeBytes < 0 ||
		expected.SHA1 == "" || len(expected.SHA1) > 128 || len(expected.AwsBucket) > 1024 || now <= 0 {
		return false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p.pool == nil {
		return false, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	multi := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		command := "UNWATCH"
		if multi {
			command = "DISCARD"
		}
		_, _ = redigo.DoContext(conn, cleanup, command)
	}()
	metadataKey, revisionKey := p.dbPrefix+prefixMetaData+expected.Id, p.dbPrefix+keyMetadataRevision
	if _, err := redigo.DoContext(conn, ctx, "WATCH", metadataKey, revisionKey); err != nil {
		return false, err
	}
	current, exists, err := p.readRecoveryFileConn(ctx, conn, expected.Id)
	if err != nil || !exists {
		return false, err
	}
	if !reflect.DeepEqual(current, expected) || (!sourceGone &&
		!((current.ExpireAt < now && !current.UnlimitedTime) ||
			(current.DownloadsRemaining < 1 && !current.UnlimitedDownloads) ||
			(current.PendingDeletion > 0 && current.PendingDeletion < now))) {
		return false, uploadquota.ErrConflict
	}
	kind, err := redigo.String(redigo.DoContext(conn, ctx, "TYPE", revisionKey))
	if err != nil {
		return false, err
	}
	if kind != "none" && kind != "string" {
		return false, uploadquota.ErrInvalid
	}
	hotlinkKey := ""
	if current.HotlinkId != "" {
		key := p.dbPrefix + prefixHotlinks + current.HotlinkId
		if _, err := redigo.DoContext(conn, ctx, "WATCH", key); err != nil {
			return false, err
		}
		kind, err := redigo.String(redigo.DoContext(conn, ctx, "TYPE", key))
		if err != nil {
			return false, err
		}
		switch kind {
		case "none":
		case "string":
			target, err := redigo.String(redigo.DoContext(conn, ctx, "GETRANGE", key, 0, 128))
			if err != nil {
				return false, err
			}
			if target == current.Id {
				hotlinkKey = key
			}
		default:
			return false, uploadquota.ErrInvalid
		}
	}
	if _, err := redigo.DoContext(conn, ctx, "MULTI"); err != nil {
		return false, err
	}
	multi = true
	if _, err := redigo.DoContext(conn, ctx, "SET", revisionKey, rand.Text()); err != nil {
		return false, err
	}
	if _, err := redigo.DoContext(conn, ctx, "DEL", metadataKey); err != nil {
		return false, err
	}
	commands := 2
	if hotlinkKey != "" {
		if _, err := redigo.DoContext(conn, ctx, "DEL", hotlinkKey); err != nil {
			return false, err
		}
		commands++
	}
	reply, err := redigo.DoContext(conn, ctx, "EXEC")
	if err != nil {
		return false, err
	}
	multi = false
	if reply == nil {
		return false, uploadquota.ErrConflict
	}
	if err := checkQuotaExec(reply, commands); err != nil {
		return false, err
	}
	return true, nil
}
