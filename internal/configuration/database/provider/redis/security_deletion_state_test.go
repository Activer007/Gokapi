//go:build test && security_deletion

package redis

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

func sec040RedisStateFixture(t *testing.T) (sec030RedisScene, models.File) {
	t.Helper()
	s := sec030RedisNew(t)
	// State revocation must remain available even if a stored physical locator
	// cannot safely be used for deletion. This test performs no physical I/O.
	file := models.File{Id: "state-file", UserId: 1, SHA1: "../legacy-locator", SizeBytes: 3,
		Name: "synthetic.txt", Size: "3 B", HotlinkId: "state-hotlink", PasswordHash: "synthetic-old-hash",
		ExpireAt: 9000, PendingDeletion: 2000, DownloadsRemaining: 8, DownloadCount: 4, UnlimitedTime: true}
	s.db.SaveMetaData(file)
	s.db.SaveHotlink(file)
	sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+prefixMetaData+file.Id, "future-field", "preserve")
	return s, file
}

func sec040RedisStateHash(t *testing.T, s sec030RedisScene, id string) map[string]string {
	t.Helper()
	value, err := redigo.StringMap(s.conn.Do("HGETALL", s.config.RedisPrefix+prefixMetaData+id))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSEC040DeletionStateRedisFieldPatches(t *testing.T) {
	for _, operation := range []models.FileStateOperation{models.FileStateExpire, models.FileStateSchedule, models.FileStateCancel, models.FileStatePassword} {
		t.Run(string(rune('0'+operation)), func(t *testing.T) {
			s, file := sec040RedisStateFixture(t)
			expected := file
			patch := models.FileStatePatch{Expected: file, Operation: operation}
			raw := sec040RedisStateHash(t, s, file.Id)
			switch operation {
			case models.FileStateExpire:
				expected.ExpireAt, expected.UnlimitedTime, expected.PendingDeletion = 0, false, 0
				raw["ExpireAt"], raw["UnlimitedTime"], raw["PendingDeletion"] = "0", "0", "0"
			case models.FileStateSchedule:
				patch.PendingDeletion, expected.PendingDeletion = 3000, 3000
				raw["PendingDeletion"] = "3000"
			case models.FileStateCancel:
				expected.PendingDeletion, raw["PendingDeletion"] = 0, "0"
			case models.FileStatePassword:
				patch.PasswordHash, expected.PasswordHash = "synthetic-new-hash", "synthetic-new-hash"
				raw["PasswordHash"] = patch.PasswordHash
			}
			ledger := sec030RedisRead(t, s.db)
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			got, found, err := s.db.ApplyFileState(context.Background(), patch)
			if err != nil || !found || !reflect.DeepEqual(got, expected) {
				t.Fatalf("state patch=%v/%v; want current patched metadata", found, err)
			}
			if !reflect.DeepEqual(raw, sec040RedisStateHash(t, s, file.Id)) ||
				!reflect.DeepEqual(ledger, sec030RedisRead(t, s.db)) ||
				reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("state patch changed unrelated fields/ledger or omitted revision")
			}
			if target, found := s.db.GetHotlink(file.HotlinkId); !found || target != file.Id {
				t.Fatal("metadata-only patch changed hotlink ownership")
			}
		})
	}
}

func TestSEC040DeletionStateRedisRejectsUncertainSnapshot(t *testing.T) {
	for _, scenario := range []string{"owner", "download-counter", "missing", "corrupt", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			s, file := sec040RedisStateFixture(t)
			ctx := context.Background()
			want := uploadquota.ErrConflict
			key := s.config.RedisPrefix + prefixMetaData + file.Id
			switch scenario {
			case "owner":
				sec030RedisDo(t, s.conn, "HSET", key, "UserId", 2)
			case "download-counter":
				sec030RedisDo(t, s.conn, "HSET", key, "DownloadCount", file.DownloadCount+1)
			case "missing":
				s.db.DeleteMetaData(file.Id)
				want = nil
			case "corrupt":
				sec030RedisDo(t, s.conn, "HSET", key, "EncryptionRedis", "invalid-gob")
				want = uploadquota.ErrInvalid
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, want = cancelled, context.Canceled
			}
			before := sec040RedisStateHash(t, s, file.Id)
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			got, found, err := s.db.ApplyFileState(ctx, models.FileStatePatch{Expected: file, Operation: models.FileStateExpire})
			if !errors.Is(err, want) || found || !reflect.DeepEqual(got, models.File{}) {
				t.Fatalf("uncertain patch=%v/%v; want false/%v and empty metadata", found, err, want)
			}
			if !reflect.DeepEqual(before, sec040RedisStateHash(t, s, file.Id)) ||
				!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("rejected patch changed metadata/revision or resurrected a deleted row")
			}
		})
	}
}

func TestSEC040DeletionStateRedisRejectsInvalidInput(t *testing.T) {
	s, file := sec040RedisStateFixture(t)
	before := sec040RedisStateHash(t, s, file.Id)
	for _, patch := range []models.FileStatePatch{
		{Expected: file, Operation: models.FileStateSchedule},
		{Expected: file, Operation: models.FileStatePassword},
		{Expected: file, Operation: models.FileStatePassword, PasswordHash: strings.Repeat("x", (64<<10)+1)},
	} {
		if _, found, err := s.db.ApplyFileState(context.Background(), patch); !errors.Is(err, uploadquota.ErrInvalid) || found {
			t.Fatal("invalid state patch was accepted", err)
		}
	}
	if !reflect.DeepEqual(before, sec040RedisStateHash(t, s, file.Id)) {
		t.Fatal("invalid patch changed metadata")
	}
}
