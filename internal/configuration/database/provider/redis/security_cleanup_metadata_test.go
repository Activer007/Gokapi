//go:build test && security_cleanup

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

func TestSEC040CleanupRedisReferencesAndRetirement(t *testing.T) {
	s := sec030RedisNew(t)
	ctx := context.Background()
	file := models.File{Id: "cleanup-file", UserId: 1, SHA1: "shared-blob", AwsBucket: "first-bucket", SizeBytes: 3,
		HotlinkId: "cleanup-hotlink", ExpireAt: 1, UnlimitedDownloads: true}
	other := models.File{Id: "other-file", UserId: 2, SHA1: file.SHA1, AwsBucket: "other-bucket", Name: strings.Repeat("x", (64<<10)+1)}
	s.db.SaveMetaData(file)
	s.db.SaveMetaData(other)
	s.db.SaveHotlink(file)
	ids, err := s.db.ReadCleanupIDs(ctx)
	if err != nil || !reflect.DeepEqual(ids, []string{file.Id, other.Id}) {
		t.Fatal("cleanup enumeration was incomplete", err)
	}
	if found, err := s.db.HasBlobReference(ctx, file); err != nil || found {
		t.Fatal("different buckets counted as one blob", err)
	}
	other.AwsBucket = file.AwsBucket
	s.db.SaveMetaData(other)
	if found, err := s.db.HasBlobReference(ctx, file); err != nil || !found {
		t.Fatal("cross-owner reference or bounded projection was lost", err)
	}
	before := sec030RedisRead(t, s.db)
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	if retired, err := s.db.RetireCleanupFile(ctx, file, 1000, false); err != nil || !retired {
		t.Fatal("expired shared metadata was not retired", err)
	}
	if _, found := s.db.GetMetaDataById(file.Id); found {
		t.Fatal("retired metadata remains")
	}
	if _, found := s.db.GetHotlink(file.HotlinkId); found {
		t.Fatal("owned hotlink survived retirement")
	}
	if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) ||
		reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
		t.Fatal("retirement changed ledger or omitted metadata revision")
	}
	if retired, err := s.db.RetireCleanupFile(ctx, file, 1000, true); err != nil || retired {
		t.Fatal("missing retirement was not idempotent", err)
	}
	// An independently reassigned hotlink must survive retirement. A confirmed
	// missing source permits retiring a live row, matching the legacy GC policy.
	file.Id, file.ExpireAt, file.UnlimitedTime = "missing-source", 2000, true
	s.db.SaveMetaData(file)
	sec030RedisDo(t, s.conn, "SET", s.config.RedisPrefix+prefixHotlinks+file.HotlinkId, other.Id)
	if retired, err := s.db.RetireCleanupFile(ctx, file, 1000, true); err != nil || !retired {
		t.Fatal("confirmed missing source was not retired", err)
	}
	if target, found := s.db.GetHotlink(file.HotlinkId); !found || target != other.Id {
		t.Fatal("retirement deleted another file's hotlink")
	}
}

func TestSEC040CleanupRedisRejectsUncertainState(t *testing.T) {
	for _, scenario := range []string{"changed-snapshot", "live-shared", "missing-expiry", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			s := sec030RedisNew(t)
			ctx := context.Background()
			file := models.File{Id: "cleanup-file", UserId: 1, SHA1: "cleanup-blob", SizeBytes: 3,
				HotlinkId: "cleanup-hotlink", ExpireAt: 1, UnlimitedDownloads: true}
			s.db.SaveMetaData(file)
			s.db.SaveHotlink(file)
			want := uploadquota.ErrConflict
			switch scenario {
			case "changed-snapshot":
				sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+prefixMetaData+file.Id, "UserId", 2)
			case "live-shared":
				file.UnlimitedTime = true
				s.db.SaveMetaData(file)
			case "missing-expiry":
				sec030RedisDo(t, s.conn, "HDEL", s.config.RedisPrefix+prefixMetaData+file.Id, "ExpireAt")
				want = uploadquota.ErrInvalid
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, want = cancelled, context.Canceled
			}
			key := s.config.RedisPrefix + prefixMetaData + file.Id
			before, err := redigo.StringMap(s.conn.Do("HGETALL", key))
			if err != nil {
				t.Fatal(err)
			}
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			if retired, err := s.db.RetireCleanupFile(ctx, file, 1000, false); !errors.Is(err, want) || retired {
				t.Fatalf("uncertain retirement=%v/%v; want false/%v", retired, err, want)
			}
			after, err := redigo.StringMap(s.conn.Do("HGETALL", key))
			if err != nil || !reflect.DeepEqual(before, after) ||
				!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("rejected retirement mutated metadata/revision", err)
			}
			if target, found := s.db.GetHotlink(file.HotlinkId); !found || target != file.Id {
				t.Fatal("rejected retirement mutated hotlink")
			}
			if scenario == "cancelled" {
				if ids, err := s.db.ReadCleanupIDs(ctx); !errors.Is(err, context.Canceled) || ids != nil {
					t.Fatal("cancelled enumeration was accepted", err)
				}
				if found, err := s.db.HasBlobReference(ctx, file); !errors.Is(err, context.Canceled) || found {
					t.Fatal("cancelled reference read was accepted", err)
				}
			}
		})
	}
}
