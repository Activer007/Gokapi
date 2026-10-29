package redis

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/blobreclaim"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	keyBlobCandidatesVersion = "blobreclaim:version"
	keyBlobCandidates        = "blobreclaim:candidates"
	blobCandidatesFormat     = "_format"
	blobCandidatesVersion    = "1"
)

// The permanent format field makes an empty collection distinguishable from a
// missing hash. It does not consume any of the MaxCandidates locator slots.
const blobCandidateStateLua = `
local function validateState()
    local marker = redis.call('TYPE', KEYS[1]).ok
    local state = redis.call('TYPE', KEYS[2]).ok
    if marker == 'none' and state == 'none' then return 'blob reclaim missing' end
    if marker ~= 'string' or state ~= 'hash' then return 'blob reclaim schema' end
    if redis.call('STRLEN', KEYS[1]) ~= 1 or redis.call('GET', KEYS[1]) ~= '1' or
       redis.call('HSTRLEN', KEYS[2], '_format') ~= 1 or redis.call('HGET', KEYS[2], '_format') ~= '1' or
       redis.call('PTTL', KEYS[1]) ~= -1 or redis.call('PTTL', KEYS[2]) ~= -1 then return 'blob reclaim schema' end
    if redis.call('HLEN', KEYS[2]) > 4097 then return 'blob reclaim full' end
    for _, key in ipairs(redis.call('HKEYS', KEYS[2])) do
        if key ~= '_format' then
            if #key ~= 64 or not string.match(key, '^[0-9a-f]+$') then return 'blob reclaim invalid' end
            local size = redis.call('HSTRLEN', KEYS[2], key)
            if size < 22 or size > 1173 then return 'blob reclaim invalid' end
            local value = redis.call('HGET', KEYS[2], key)
            if string.byte(value, 1) ~= 1 then return 'blob reclaim invalid' end
            local hashlen = string.byte(value, 2) * 256 + string.byte(value, 3)
            local bucketlen = string.byte(value, 4) * 256 + string.byte(value, 5)
            if hashlen < 1 or hashlen > 128 or bucketlen > 1024 or #value ~= 21 + hashlen + bucketlen then return 'blob reclaim invalid' end
            if bucketlen == 0 then
                local name = string.sub(value, 6, 5 + hashlen)
                if name == '.' or name == '..' or string.sub(name, -1) == '.' or not string.match(name, '^[A-Za-z0-9._%-]+$') then return 'blob reclaim invalid' end
                local base = string.upper(string.match(name, '^([^.]*)'))
                if base == 'CON' or base == 'PRN' or base == 'AUX' or base == 'NUL' or string.match(base, '^COM[1-9]$') or string.match(base, '^LPT[1-9]$') then return 'blob reclaim invalid' end
            end
        end
    end
    return nil
end
local problem = validateState()
if problem then return redis.error_reply(problem) end
`

var readBlobCandidatesScript = redigo.NewScript(2, blobCandidateStateLua+`
return redis.call('HGETALL', KEYS[2])
`)
var readBlobCandidateScript = redigo.NewScript(2, blobCandidateStateLua+`
return redis.call('HGET', KEYS[2], ARGV[1])
`)
var retireBlobCandidateScript = redigo.NewScript(2, blobCandidateStateLua+`
local value = redis.call('HGET', KEYS[2], ARGV[1])
if not value then return 0 end
if value ~= ARGV[2] then return -1 end
return redis.call('HDEL', KEYS[2], ARGV[1])
`)
var importBlobCandidatesScript = redigo.NewScript(2, blobCandidateStateLua+`
if redis.call('HLEN', KEYS[2]) ~= 1 then return redis.error_reply('blob reclaim conflict') end
if #ARGV > 8192 or #ARGV % 2 ~= 0 then return redis.error_reply('blob reclaim invalid') end
for i = 1, #ARGV, 2 do
    if #ARGV[i] ~= 64 or not string.match(ARGV[i], '^[0-9a-f]+$') or #ARGV[i+1] < 22 or #ARGV[i+1] > 1173 then return redis.error_reply('blob reclaim invalid') end
end
for i = 1, #ARGV, 2 do redis.call('HSET', KEYS[2], ARGV[i], ARGV[i+1]) end
return 1
`)
var initBlobCandidatesScript = redigo.NewScript(2, `
if redis.call('TYPE', KEYS[1]).ok ~= 'none' or redis.call('TYPE', KEYS[2]).ok ~= 'none' then return redis.error_reply('blob reclaim conflict') end
redis.call('HSET', KEYS[2], '_format', '1')
redis.call('SET', KEYS[1], '1')
return 1
`)

func blobCandidateRedisError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var redisErr redigo.Error
	if errors.As(err, &redisErr) {
		switch strings.TrimPrefix(string(redisErr), "ERR ") {
		case "blob reclaim missing":
			return errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound)
		case "blob reclaim schema":
			return uploadquota.ErrSchema
		case "blob reclaim full":
			return uploadquota.ErrFull
		case "blob reclaim conflict":
			return uploadquota.ErrConflict
		case "blob reclaim invalid":
			return uploadquota.ErrInvalid
		}
	}
	return err
}

func validBlobCandidateKey(key string) bool {
	if len(key) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(key)
	return err == nil && hex.EncodeToString(decoded) == key
}

func (p DatabaseProvider) blobCandidateConnection(ctx context.Context) (redigo.Conn, error) {
	if ctx == nil {
		return nil, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.pool == nil {
		return nil, uploadquota.ErrSchema
	}
	return p.pool.GetContext(ctx)
}

func (p DatabaseProvider) readBlobCandidatesConn(ctx context.Context, conn redigo.Conn) ([]blobreclaim.Candidate, error) {
	values, err := redigo.ByteSlices(readBlobCandidatesScript.DoContext(ctx, conn, p.dbPrefix+keyBlobCandidatesVersion, p.dbPrefix+keyBlobCandidates))
	if err != nil {
		return nil, blobCandidateRedisError(ctx, err)
	}
	if len(values) < 2 || len(values)%2 != 0 || len(values) > 2*(blobreclaim.MaxCandidates+1) {
		return nil, uploadquota.ErrInvalid
	}
	result := make([]blobreclaim.Candidate, 0, len(values)/2-1)
	format := false
	for i := 0; i < len(values); i += 2 {
		key := string(values[i])
		if key == blobCandidatesFormat {
			if format || string(values[i+1]) != blobCandidatesVersion {
				return nil, uploadquota.ErrSchema
			}
			format = true
			continue
		}
		candidate, err := blobreclaim.Decode(values[i+1])
		if err != nil || !validBlobCandidateKey(key) || candidate.Key() != key {
			return nil, uploadquota.ErrInvalid
		}
		result = append(result, candidate)
	}
	if !format {
		return nil, uploadquota.ErrSchema
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key() < result[j].Key() })
	return result, nil
}

func (p DatabaseProvider) ReadBlobCandidates(ctx context.Context) ([]blobreclaim.Candidate, error) {
	conn, err := p.blobCandidateConnection(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return p.readBlobCandidatesConn(ctx, conn)
}

func (p DatabaseProvider) ReadBlobCandidate(ctx context.Context, key string) (blobreclaim.Candidate, bool, error) {
	if !validBlobCandidateKey(key) {
		return blobreclaim.Candidate{}, false, uploadquota.ErrInvalid
	}
	conn, err := p.blobCandidateConnection(ctx)
	if err != nil {
		return blobreclaim.Candidate{}, false, err
	}
	defer conn.Close()
	encoded, err := redigo.Bytes(readBlobCandidateScript.DoContext(ctx, conn, p.dbPrefix+keyBlobCandidatesVersion, p.dbPrefix+keyBlobCandidates, key))
	if errors.Is(err, redigo.ErrNil) {
		return blobreclaim.Candidate{}, false, nil
	}
	if err != nil {
		return blobreclaim.Candidate{}, false, blobCandidateRedisError(ctx, err)
	}
	candidate, err := blobreclaim.Decode(encoded)
	if err != nil || candidate.Key() != key {
		return blobreclaim.Candidate{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return blobreclaim.Candidate{}, false, err
	}
	return candidate, true, nil
}

func (p DatabaseProvider) RetireBlobCandidate(ctx context.Context, expected blobreclaim.Candidate) (bool, error) {
	encoded, err := blobreclaim.Encode(expected)
	if err != nil {
		return false, err
	}
	conn, err := p.blobCandidateConnection(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	removed, err := redigo.Int(retireBlobCandidateScript.DoContext(ctx, conn, p.dbPrefix+keyBlobCandidatesVersion, p.dbPrefix+keyBlobCandidates, expected.Key(), encoded))
	if err != nil {
		return false, blobCandidateRedisError(ctx, err)
	}
	if removed == -1 {
		return false, uploadquota.ErrConflict
	}
	if removed != 0 && removed != 1 {
		return false, uploadquota.ErrInvalid
	}
	return removed == 1, nil
}

func (p DatabaseProvider) HasAnyBlobReference(ctx context.Context, candidate blobreclaim.Candidate) (bool, error) {
	if ctx == nil {
		return false, uploadquota.ErrInvalid
	}
	if err := candidate.Validate(); err != nil {
		return false, err
	}
	return p.hasBlobReference(ctx, candidate.SHA1, candidate.AwsBucket, "")
}

// Import is an offline operation into an already initialized empty collection.
// It never replaces existing candidates, and an unknown script result is returned.
func (p DatabaseProvider) ImportBlobCandidates(ctx context.Context, candidates []blobreclaim.Candidate) error {
	if len(candidates) > blobreclaim.MaxCandidates {
		return uploadquota.ErrFull
	}
	args := redigo.Args{p.dbPrefix + keyBlobCandidatesVersion, p.dbPrefix + keyBlobCandidates}
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		encoded, err := blobreclaim.Encode(candidate)
		if err != nil || seen[candidate.Key()] {
			return uploadquota.ErrInvalid
		}
		seen[candidate.Key()] = true
		args = args.Add(candidate.Key(), encoded)
	}
	conn, err := p.blobCandidateConnection(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = p.readBlobCandidatesConn(ctx, conn)
	if err != nil {
		return err
	}
	_, err = importBlobCandidatesScript.DoContext(ctx, conn, args...)
	return blobCandidateRedisError(ctx, err)
}

// Only fresh database initialization and the explicit schema upgrade call this.
func (p DatabaseProvider) initBlobCandidates() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := p.blobCandidateConnection(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = p.readBlobCandidatesConn(ctx, conn)
	if err == nil {
		return nil
	}
	if !errors.Is(err, uploadquota.ErrSchema) || !errors.Is(err, uploadquota.ErrNotFound) {
		return err
	}
	_, err = initBlobCandidatesScript.DoContext(ctx, conn, p.dbPrefix+keyBlobCandidatesVersion, p.dbPrefix+keyBlobCandidates)
	if err != nil && !errors.Is(blobCandidateRedisError(ctx, err), uploadquota.ErrConflict) {
		return blobCandidateRedisError(ctx, err)
	}
	_, err = p.readBlobCandidatesConn(ctx, conn)
	return err
}

// One Lua command prechecks state before writing the candidate, then content
// and optional expiry. Registration failure cannot overwrite content. Runtime
// failures or lost replies do not promise rollback of earlier Lua/EXEC writes.
const replaceWithBlobCandidateLua = blobCandidateStateLua + `
if redis.call('TYPE', KEYS[3]).ok ~= 'hash' or redis.call('TYPE', KEYS[4]).ok ~= 'hash' then return redis.error_reply('blob reclaim invalid') end
if #ARGV ~= 17 or (ARGV[3] ~= '0' and ARGV[3] ~= '1') then return redis.error_reply('blob reclaim invalid') end
local fields = {'Name', 'Size', 'SHA1', 'ContentType', 'AwsBucket', 'SizeBytes', 'EncryptionRedis'}
for i, field in ipairs(fields) do if ARGV[2*i+2] ~= field then return redis.error_reply('blob reclaim invalid') end end
if ARGV[1] ~= '' then
    if #ARGV[1] ~= 64 or not string.match(ARGV[1], '^[0-9a-f]+$') or #ARGV[2] < 22 or #ARGV[2] > 1173 then return redis.error_reply('blob reclaim invalid') end
    if redis.call('HEXISTS', KEYS[2], ARGV[1]) == 0 and redis.call('HLEN', KEYS[2]) >= 4097 then return redis.error_reply('blob reclaim full') end
end
if ARGV[3] == '1' and KEYS[3] == KEYS[4] then return redis.error_reply('blob reclaim invalid') end
if ARGV[1] ~= '' then redis.call('HSET', KEYS[2], ARGV[1], ARGV[2]) end
redis.call('HSET', KEYS[3], unpack(ARGV, 4, 17))
if ARGV[3] == '1' then redis.call('HSET', KEYS[4], 'ExpireAt', 0, 'UnlimitedTime', 0) end
return 1
`

func (p DatabaseProvider) prepareBlobReplacement(ctx context.Context, conn redigo.Conn, change uploadquota.Change, patchArgs redigo.Args) (redigo.Args, error) {
	if _, err := redigo.DoContext(conn, ctx, "WATCH", p.dbPrefix+keyBlobCandidatesVersion, p.dbPrefix+keyBlobCandidates); err != nil {
		return nil, err
	}
	candidates, err := p.readBlobCandidatesConn(ctx, conn)
	if err != nil {
		return nil, err
	}
	field, value := "", []byte{}
	if change.Target.SHA1 != change.Source.SHA1 || change.Target.AwsBucket != change.Source.AwsBucket {
		candidate, err := blobreclaim.New(change.Target.SHA1, change.Target.AwsBucket)
		if err != nil {
			return nil, err
		}
		field = candidate.Key()
		present := false
		for _, current := range candidates {
			if current.Key() == field {
				present = true
				break
			}
		}
		if !present && len(candidates) >= blobreclaim.MaxCandidates {
			return nil, uploadquota.ErrFull
		}
		value, err = blobreclaim.Encode(candidate)
		if err != nil {
			return nil, err
		}
	}
	if len(patchArgs) != 15 {
		return nil, uploadquota.ErrInvalid
	}
	deleteFlag := "0"
	if change.DeleteSource {
		deleteFlag = "1"
	}
	args := redigo.Args{replaceWithBlobCandidateLua, 4, p.dbPrefix + keyBlobCandidatesVersion, p.dbPrefix + keyBlobCandidates,
		p.dbPrefix + prefixMetaData + change.Target.Id, p.dbPrefix + prefixMetaData + change.Source.Id, field, value, deleteFlag}
	return append(args, patchArgs[1:]...), nil
}
