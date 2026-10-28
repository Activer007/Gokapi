package redis

import (
	"github.com/forceu/gokapi/internal/helper"
	redigo "github.com/gomodule/redigo/redis"
)

// Telemetry must not create records or restore fields from an earlier snapshot.
// The existence check and update are atomic with respect to deletion; HSET on
// the existing hash also preserves its TTL. Pass keys/values as arguments, not
// interpolated Lua, so database prefixes remain opaque.
var updateExistingUsage = redigo.NewScript(1, `
if redis.call('EXISTS', KEYS[1]) == 1 then
    return redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
end
return 0
`)

func (p DatabaseProvider) updateUsage(id, field string, timestamp int64) {
	conn := p.pool.Get()
	defer conn.Close()
	_, err := updateExistingUsage.Do(conn, p.dbPrefix+id, field, timestamp)
	helper.Check(err)
}
