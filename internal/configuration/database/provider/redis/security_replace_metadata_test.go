//go:build test && security_replace

package redis

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

func sec030ReplaceRedisFixture(t *testing.T) (sec030RedisScene, uploadquota.Change) {
	t.Helper()
	s := sec030RedisNew(t)
	target := models.File{Id: "replace-target", UserId: 1, SizeBytes: 3, SHA1: "synthetic-old-blob", Name: "old.txt", Size: "3 B",
		ContentType: "text/plain", PasswordHash: "synthetic-target-password-hash", HotlinkId: "target-hotlink",
		ExpireAt: 2000, DownloadsRemaining: 10, DownloadCount: 7, UploadDate: 800}
	source := models.File{Id: "replace-source", UserId: 2, SizeBytes: 5, SHA1: "synthetic-new-blob", Name: "new.bin", Size: "5 B",
		ContentType: "application/octet-stream", UploadRequestId: "original-source-request", ExpireAt: 3000, DownloadsRemaining: 20,
		Encryption: models.EncryptionInfo{IsEncrypted: true, DecryptionKey: bytes.Repeat([]byte{0x41}, 32), Nonce: bytes.Repeat([]byte{0x42}, 12)}}
	s.db.SaveMetaData(target)
	s.db.SaveMetaData(source)
	s.db.SaveHotlink(target)
	return s, uploadquota.Change{Action: uploadquota.ReplaceMetadata, Target: &target, Source: &source, Now: 1000,
		Upload: uploadquota.Reservation{ID: target.Id, UserID: target.UserId},
		Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 100}, Instance: uploadquota.Usage{Bytes: 100}}}
}

func sec030ReplaceRedisHash(t *testing.T, s sec030RedisScene, id string) map[string]string {
	t.Helper()
	value, err := redigo.StringMap(s.conn.Do("HGETALL", s.config.RedisPrefix+prefixMetaData+id))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSEC030ReplaceRedisPatchAndAtomicExpiry(t *testing.T) {
	s, change := sec030ReplaceRedisFixture(t)
	change.DeleteSource = true
	sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+prefixMetaData+change.Target.Id,
		"PasswordHash", "synthetic-current-password-hash", "ExpireAt", 4000,
		"DownloadCount", 8, "DownloadsRemaining", 9, "future-field", "preserve")
	expected := sec030ReplaceRedisHash(t, s, change.Target.Id)
	source := sec030ReplaceRedisHash(t, s, change.Source.Id)
	for _, field := range []string{"Name", "Size", "SHA1", "ContentType", "AwsBucket", "SizeBytes", "EncryptionRedis"} {
		expected[field] = source[field]
	}
	source["ExpireAt"], source["UnlimitedTime"] = "0", "0"
	before := sec030RedisRead(t, s.db)
	record, err := s.db.ApplyUploadQuota(context.Background(), change)
	if err != nil || record != (uploadquota.Reservation{}) {
		t.Fatal("replacement rejected or created upload state", err)
	}
	if !reflect.DeepEqual(expected, sec030ReplaceRedisHash(t, s, change.Target.Id)) ||
		!reflect.DeepEqual(source, sec030ReplaceRedisHash(t, s, change.Source.Id)) || !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) {
		t.Fatal("replacement overwrote target policy/current fields, source metadata or ledger")
	}
	if id, found := s.db.GetHotlink(change.Target.HotlinkId); !found || id != change.Target.Id {
		t.Fatal("replacement changed the existing target hotlink")
	}
	usage, err := uploadquota.Measure(s.db.GetAllMetadata())
	if err != nil || usage.Users[change.Target.UserId].Bytes != 5 || usage.Requests[change.Source.UploadRequestId].Bytes != 5 {
		t.Fatal("replacement prematurely refunded expired source metadata", err)
	}
}

func TestSEC030ReplaceRedisRejectionLeavesSourceLive(t *testing.T) {
	for _, scenario := range []string{"quota", "oversize-text"} {
		t.Run(scenario, func(t *testing.T) {
			s, change := sec030ReplaceRedisFixture(t)
			change.DeleteSource = true
			want := uploadquota.ErrQuota
			if scenario == "quota" {
				change.Limits.User.Bytes = 4
			} else {
				sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+prefixMetaData+change.Source.Id,
					"Name", strings.Repeat("n", uploadquota.MaxMetadataTextBytes+1))
				want = uploadquota.ErrInvalid
			}
			target, source := sec030ReplaceRedisHash(t, s, change.Target.Id), sec030ReplaceRedisHash(t, s, change.Source.Id)
			before := sec030RedisRead(t, s.db)
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			if _, err := s.db.ApplyUploadQuota(context.Background(), change); !errors.Is(err, want) {
				t.Errorf("replacement rejection=%v; want %v", err, want)
			}
			if !reflect.DeepEqual(target, sec030ReplaceRedisHash(t, s, change.Target.Id)) ||
				!reflect.DeepEqual(source, sec030ReplaceRedisHash(t, s, change.Source.Id)) ||
				!reflect.DeepEqual(before, sec030RedisRead(t, s.db)) ||
				!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("rejected replacement partially patched metadata, source expiry or ledger")
			}
		})
	}
}

func TestSEC030ReplaceRedisNoGrowthOverLimit(t *testing.T) {
	for _, size := range []int64{3, 1} {
		s, change := sec030ReplaceRedisFixture(t)
		change.Source.SizeBytes = size
		s.db.SaveMetaData(*change.Source)
		change.Limits = uploadquota.Limits{}
		before := sec030RedisRead(t, s.db)
		if _, err := s.db.ApplyUploadQuota(context.Background(), change); err != nil {
			t.Fatal("equal/smaller replacement could not reduce over-limit usage", err)
		}
		file, found := s.db.GetMetaDataById(change.Target.Id)
		if !found || file.SizeBytes != size || !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) {
			t.Fatal("no-growth replacement lost its size or created upload state")
		}
	}
}

func TestSEC030ReplaceRedisWatchedAuthorizationChange(t *testing.T) {
	for _, changing := range []string{"source", "target"} {
		t.Run(changing, func(t *testing.T) {
			s, change := sec030ReplaceRedisFixture(t)
			change.DeleteSource = true
			key := s.config.RedisPrefix + prefixMetaData + change.Source.Id
			if changing == "target" {
				key = s.config.RedisPrefix + prefixMetaData + change.Target.Id
			}
			before := sec030RedisRead(t, s.db)
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			var wrapped *sec030RedisTypeRaceConn
			pool := &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
				conn, err := redigo.DialContext(ctx, "tcp", s.config.HostUrl)
				if err != nil {
					return nil, err
				}
				wrapped = &sec030RedisTypeRaceConn{Conn: conn, target: key, beforeMulti: func() {
					sec030RedisDo(t, s.conn, "HSET", key, "UserId", 3)
				}}
				return wrapped, nil
			}}
			t.Cleanup(func() { _ = pool.Close() })
			db := DatabaseProvider{pool: pool, dbPrefix: s.config.RedisPrefix}
			_, err := db.ApplyUploadQuota(context.Background(), change)
			if !errors.Is(err, uploadquota.ErrConflict) || wrapped == nil || !wrapped.watchedTarget || !wrapped.injected || wrapped.executions != 1 {
				t.Errorf("authorization snapshot change was not rejected through WATCH/retry: %v", err)
			}
			if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) ||
				!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) ||
				sec030ReplaceRedisHash(t, s, change.Target.Id)["SHA1"] != change.Target.SHA1 ||
				sec030ReplaceRedisHash(t, s, change.Source.Id)["ExpireAt"] != "3000" {
				t.Fatal("authorization race partially replaced target or expired source")
			}
		})
	}
}
