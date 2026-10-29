//go:build test && security_owner

package redis

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

func sec030OwnerRedisFixture(t *testing.T) (sec030RedisScene, uploadquota.Change) {
	t.Helper()
	s := sec030RedisNew(t)
	source := models.File{Id: "owner-source", UserId: 1, SizeBytes: 3, UploadRequestId: "original-request",
		SHA1: "synthetic-shared-blob", Name: "before.txt", HotlinkId: "owner-hotlink", UnlimitedTime: true, UnlimitedDownloads: true}
	s.db.SaveMetaData(source)
	s.db.SaveHotlink(source)
	s.db.SaveUser(models.User{Id: 2, Name: "synthetic-target", UserLevel: models.UserLevelUser}, false)
	return s, uploadquota.Change{Action: uploadquota.TransferOwner, Source: &source, Now: 1000,
		Upload: uploadquota.Reservation{ID: source.Id, UserID: 2}, Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 3, Files: 1}}}
}

func TestSEC030OwnerRedisPatchesOnlyCurrentOwner(t *testing.T) {
	s, change := sec030OwnerRedisFixture(t)
	key := s.config.RedisPrefix + prefixMetaData + change.Source.Id
	// These changes occurred after the caller's authorized File snapshot. They
	// must survive because transfer patches UserId instead of saving that File.
	sec030RedisDo(t, s.conn, "HSET", key, "Name", "after.txt", "PasswordHash", "synthetic-current-password-hash",
		"DownloadCount", 23, "EncryptionRedis", "synthetic-opaque-current-encryption", "future-field", "preserve")
	expected, err := redigo.StringMap(s.conn.Do("HGETALL", key))
	if err != nil {
		t.Fatal(err)
	}
	expected["UserId"] = "2"
	before := sec030RedisRead(t, s.db)
	record, err := s.db.ApplyUploadQuota(context.Background(), change)
	if err != nil || record != (uploadquota.Reservation{}) {
		t.Fatal("owner-only transfer rejected or created an upload receipt", err)
	}
	current, err := redigo.StringMap(s.conn.Do("HGETALL", key))
	if err != nil || !reflect.DeepEqual(expected, current) || !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) {
		t.Fatal("owner transfer changed current fields, original request or ledger", err)
	}
	if id, found := s.db.GetHotlink(change.Source.HotlinkId); !found || id != change.Source.Id {
		t.Fatal("owner transfer changed the original hotlink")
	}
	// Same-owner retries are allowed with literal zero capacity and no parent
	// request record. Neither operation adds instance/request/staging usage.
	change.Source.UserId, change.Limits.User = 2, uploadquota.Usage{}
	if _, err := s.db.ApplyUploadQuota(context.Background(), change); err != nil {
		t.Fatal("same-owner retry unexpectedly required new capacity", err)
	}
	sec030RedisDo(t, s.conn, "HSET", key, "PendingDeletion", 1)
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	if _, err := s.db.ApplyUploadQuota(context.Background(), change); !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatal("pending deletion was accepted as an owner transfer", err)
	}
	if !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) ||
		!reflect.DeepEqual(before, sec030RedisRead(t, s.db)) {
		t.Fatal("rejected pending source changed revision or ledger")
	}
}

func TestSEC030OwnerRedisWatchedDisappearance(t *testing.T) {
	for _, disappearing := range []string{"source", "target"} {
		t.Run(disappearing, func(t *testing.T) {
			s, change := sec030OwnerRedisFixture(t)
			sourceKey := s.config.RedisPrefix + prefixMetaData + change.Source.Id
			targetKey := s.config.RedisPrefix + prefixUsers + "2"
			key := sourceKey
			if disappearing == "target" {
				key = targetKey
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
					// Deliberately bypass revision notification. Watching the exact
					// source and target keys must abort EXEC before any patch.
					sec030RedisDo(t, s.conn, "DEL", key)
				}}
				return wrapped, nil
			}}
			t.Cleanup(func() { _ = pool.Close() })
			db := DatabaseProvider{pool: pool, dbPrefix: s.config.RedisPrefix}
			_, err := db.ApplyUploadQuota(context.Background(), change)
			if !errors.Is(err, uploadquota.ErrConflict) || wrapped == nil || !wrapped.watchedTarget || !wrapped.injected || wrapped.executions != 1 {
				t.Errorf("disappearance was not rejected through WATCH/retry: %v", err)
			}
			if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) ||
				!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("disappearance partially changed the ledger or revision")
			}
			if n, err := redigo.Int(s.conn.Do("EXISTS", key)); err != nil || n != 0 {
				t.Fatal("owner transfer resurrected a deleted source or target", err)
			}
			if disappearing == "target" {
				if owner, err := redigo.Int(s.conn.Do("HGET", sourceKey, "UserId")); err != nil || owner != change.Source.UserId {
					t.Fatal("owner transfer patched source after target disappeared", err)
				}
			}
			if id, found := s.db.GetHotlink(change.Source.HotlinkId); !found || id != change.Source.Id {
				t.Fatal("rejected owner transfer changed the original hotlink")
			}
		})
	}
}
