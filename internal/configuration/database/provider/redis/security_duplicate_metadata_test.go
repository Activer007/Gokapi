//go:build test && security_duplicate

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

func sec030RedisDuplicate(t *testing.T) (sec030RedisScene, uploadquota.Change) {
	t.Helper()
	s := sec030RedisNew(t)
	source := models.File{Id: "duplicate-source", UserId: 1, SizeBytes: 3, SHA1: "synthetic-shared-blob",
		UnlimitedTime: true, UnlimitedDownloads: true, HotlinkId: "source-hotlink",
		Encryption: models.EncryptionInfo{IsEncrypted: true, DecryptionKey: bytes.Repeat([]byte{0x41}, 32), Nonce: bytes.Repeat([]byte{0x42}, 12)}}
	s.db.SaveMetaData(source)
	s.db.SaveHotlink(source)
	file := source
	file.Id, file.UserId, file.HotlinkId = "duplicate-target", 2, "duplicate-hotlink"
	change := sec030RedisChange(file.Id, file.SizeBytes)
	change.Action, change.Source, change.File = uploadquota.DuplicateMetadata, &source, &file
	change.Upload.UserID, change.Upload.RequestID, change.Upload.StagingBytes = file.UserId, "", 0
	return s, change
}

func TestSEC030DuplicateRedisSourceRejectionAtomic(t *testing.T) {
	for _, scenario := range []string{"missing", "owner", "blob", "invalid-integer", "oversize-encryption", "trailing-gob"} {
		t.Run(scenario, func(t *testing.T) {
			s, change := sec030RedisDuplicate(t)
			key := s.config.RedisPrefix + prefixMetaData + change.Source.Id
			want := uploadquota.ErrConflict
			switch scenario {
			case "missing":
				sec030RedisDo(t, s.conn, "DEL", key)
			case "owner":
				sec030RedisDo(t, s.conn, "HSET", key, "UserId", 3)
			case "blob":
				sec030RedisDo(t, s.conn, "HSET", key, "SHA1", "different-synthetic-blob")
			case "invalid-integer":
				sec030RedisDo(t, s.conn, "HSET", key, "SizeBytes", "1.5")
				want = uploadquota.ErrInvalid
			case "oversize-encryption":
				sec030RedisDo(t, s.conn, "HSET", key, "EncryptionRedis", strings.Repeat("x", 4097))
				want = uploadquota.ErrInvalid
			case "trailing-gob":
				data, err := redigo.Bytes(s.conn.Do("HGET", key, "EncryptionRedis"))
				if err != nil {
					t.Fatal(err)
				}
				sec030RedisDo(t, s.conn, "HSET", key, "EncryptionRedis", append(data, 0))
				want = uploadquota.ErrInvalid
			}
			before := sec030RedisRead(t, s.db)
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			// Inspect raw hashes because the intentionally corrupt gob/integer
			// must never be passed through the legacy panic-on-error getter.
			source, err := redigo.StringMap(s.conn.Do("HGETALL", key))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ApplyUploadQuota(context.Background(), change); !errors.Is(err, want) {
				t.Errorf("source rejection error=%v; want %v", err, want)
			}
			afterSource, err := redigo.StringMap(s.conn.Do("HGETALL", key))
			if err != nil || !reflect.DeepEqual(source, afterSource) || !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) ||
				!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("rejected duplicate changed source, ledger or revision", err)
			}
			if n, err := redigo.Int(s.conn.Do("EXISTS", s.config.RedisPrefix+prefixMetaData+change.File.Id, s.config.RedisPrefix+prefixHotlinks+change.File.HotlinkId)); err != nil || n != 0 {
				t.Fatal("rejected duplicate partially published metadata or hotlink", err)
			}
			if id, found := s.db.GetHotlink(change.Source.HotlinkId); !found || id != change.Source.Id {
				t.Fatal("rejected duplicate changed the original hotlink")
			}
		})
	}
}

func TestSEC030DuplicateRedisGobRoundTrip(t *testing.T) {
	s, change := sec030RedisDuplicate(t)
	before := sec030RedisRead(t, s.db)
	// Display/credential fields are deliberately excluded from the bounded read.
	sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+prefixMetaData+change.Source.Id,
		"Name", strings.Repeat("n", 64<<10), "PasswordHash", strings.Repeat("p", 64<<10))
	record, err := s.db.ApplyUploadQuota(context.Background(), change)
	if err != nil || !reflect.DeepEqual(record, uploadquota.Reservation{}) {
		t.Fatal("existing gob encryption was not accepted without a reservation", err)
	}
	file, found := s.db.GetMetaDataById(change.File.Id)
	if !found || !reflect.DeepEqual(file.Encryption, change.Source.Encryption) || file.UserId != change.File.UserId ||
		file.SHA1 != change.Source.SHA1 || !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) {
		t.Fatal("duplicate changed encryption/shared locator or created upload state")
	}
	if id, found := s.db.GetHotlink(change.File.HotlinkId); !found || id != change.File.Id {
		t.Fatal("successful duplicate did not atomically publish its hotlink")
	}
}

func TestSEC030DuplicateRedisWatchedSourceChange(t *testing.T) {
	s, change := sec030RedisDuplicate(t)
	before := sec030RedisRead(t, s.db)
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	key := s.config.RedisPrefix + prefixMetaData + change.Source.Id
	var wrapped *sec030RedisTypeRaceConn
	pool := &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
		conn, err := redigo.DialContext(ctx, "tcp", s.config.HostUrl)
		if err != nil {
			return nil, err
		}
		wrapped = &sec030RedisTypeRaceConn{Conn: conn, target: key, beforeMulti: func() {
			// An external writer bypasses the shared revision after the source
			// projection. Its direct WATCH must abort EXEC, then recheck ownership.
			sec030RedisDo(t, s.conn, "HSET", key, "UserId", 3)
		}}
		return wrapped, nil
	}}
	t.Cleanup(func() { _ = pool.Close() })
	db := DatabaseProvider{pool: pool, dbPrefix: s.config.RedisPrefix}
	_, err := db.ApplyUploadQuota(context.Background(), change)
	if !errors.Is(err, uploadquota.ErrConflict) || wrapped == nil || !wrapped.watchedTarget || !wrapped.injected || wrapped.executions != 1 {
		t.Errorf("source change was not rejected through WATCH/retry: %v", err)
	}
	if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) ||
		!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
		t.Fatal("source race partially changed ledger or revision")
	}
	if n, err := redigo.Int(s.conn.Do("EXISTS", s.config.RedisPrefix+prefixMetaData+change.File.Id, s.config.RedisPrefix+prefixHotlinks+change.File.HotlinkId)); err != nil || n != 0 {
		t.Fatal("source race partially published metadata or hotlink", err)
	}
}
