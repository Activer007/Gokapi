//go:build test

package redis

import (
	"context"
	"errors"
	redigo "github.com/gomodule/redigo/redis"
	"reflect"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
)

func sec030RequestPolicyFixture(t *testing.T, action uploadquota.Action) (DatabaseProvider, uploadquota.Change, models.FileRequest) {
	t.Helper()
	db := sec030RedisNew(t).db
	request := models.FileRequest{Id: "request", UserId: 1, MaxFiles: 10, MaxSize: 8, Expiry: 4600}
	db.SaveFileRequest(request)
	change := sec030RedisChange("request-upload", 2<<20)
	change.Upload.ID, change.Upload.RequestID = "request-upload", request.Id
	change.Upload.BytesKnown, change.Upload.Bytes, change.Upload.StagingBytes = true, 2<<20, 4<<20
	change.Limits = uploadquota.Limits{User: uploadquota.Usage{Bytes: 100 << 20, Files: 100},
		Request: uploadquota.Usage{Bytes: 100 << 20, Files: 100}, Instance: uploadquota.Usage{Bytes: 200 << 20, Files: 200},
		UserActive: 100, RequestActive: 10, InstanceActive: 200, StagingBytes: 200 << 20}
	apply := func(command uploadquota.Change) {
		t.Helper()
		if _, err := db.ApplyUploadQuota(context.Background(), command); err != nil {
			t.Fatal(err)
		}
	}
	if action != uploadquota.Reserve {
		initial := change
		if action == uploadquota.Bind {
			initial.Upload.BytesKnown, initial.Upload.Bytes, initial.Upload.StagingBytes = false, 0, 0
		}
		apply(initial)
	}
	if action == uploadquota.Commit || action == uploadquota.ReleaseStaging {
		claim := change
		claim.Action, claim.FileID = uploadquota.Claim, "request-file"
		apply(claim)
		change.File = &models.File{Id: "request-file", UserId: 1, UploadRequestId: request.Id, SizeBytes: change.Upload.Bytes, SHA1: "synthetic"}
		if action == uploadquota.ReleaseStaging {
			commit := change
			commit.Action = uploadquota.Commit
			apply(commit)
		}
	}
	change.Action, change.FileID, change.CheckRequestPolicy = action, "request-file", true
	return db, change, request
}

func sec030RequestPolicyReject(t *testing.T, db DatabaseProvider, change uploadquota.Change, want error) {
	t.Helper()
	before, err := db.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := db.GetAllMetadata()
	_, err = db.ApplyUploadQuota(context.Background(), change)
	if err == nil || (want != nil && !errors.Is(err, want)) {
		t.Errorf("request policy error=%v; want %v", err, want)
	}
	after, err := db.ReadUploadQuota(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(files, db.GetAllMetadata()) {
		t.Fatal("rejected current request policy partially changed ledger or metadata", err)
	}
}

func TestSEC030RequestPolicyRedisCurrentPolicy(t *testing.T) {
	for _, action := range []uploadquota.Action{uploadquota.Reserve, uploadquota.Bind, uploadquota.Claim, uploadquota.Commit} {
		for _, scenario := range []string{"missing", "owner", "expired", "count", "single-file", "total"} {
			t.Run(string(action)+"/"+scenario, func(t *testing.T) {
				db, change, request := sec030RequestPolicyFixture(t, action)
				want := uploadquota.ErrQuota
				switch scenario {
				case "missing":
					db.DeleteFileRequest(request)
					want = uploadquota.ErrNotFound
				case "owner":
					request.UserId = 2
					db.SaveFileRequest(request)
					want = uploadquota.ErrConflict
				case "expired":
					request.Expiry = change.Now - 1
					db.SaveFileRequest(request)
					want = uploadquota.ErrExpired
				case "count":
					request.MaxFiles = 1
					db.SaveFileRequest(request)
					// Transfer and pending deletion cannot remove the original request charge.
					db.SaveMetaData(models.File{Id: "transferred-pending", UserId: 99, UploadRequestId: request.Id, SizeBytes: 1, PendingDeletion: 999})
				case "single-file":
					request.MaxSize = 1
					db.SaveFileRequest(request)
				case "total":
					change.Limits.Request.Bytes = change.Upload.Bytes
					db.SaveMetaData(models.File{Id: "transferred-pending", UserId: 99, UploadRequestId: request.Id, SizeBytes: 1, PendingDeletion: 999})
				}
				sec030RequestPolicyReject(t, db, change, want)
			})
		}
	}
}

func TestSEC030RequestPolicyRedisLegacyZeroAndExpirySecond(t *testing.T) {
	for _, expiry := range []int64{0, 1000} {
		db, change, request := sec030RequestPolicyFixture(t, uploadquota.Reserve)
		request.MaxFiles, request.MaxSize, request.Expiry = 0, 0, expiry
		db.SaveFileRequest(request)
		// The provider must resolve old request zero to a finite enclosing count.
		change.Limits.Request.Files = 0
		if _, err := db.ApplyUploadQuota(context.Background(), change); err != nil {
			t.Fatal("legacy zero or exact expiry second changed", err)
		}
	}
}

func TestSEC030RequestPolicyRedisCleanupAfterRequestDeletion(t *testing.T) {
	for _, action := range []uploadquota.Action{uploadquota.Abort, uploadquota.ReleaseStaging} {
		db, change, request := sec030RequestPolicyFixture(t, action)
		db.DeleteFileRequest(request)
		change.CleanupConfirmed, change.Terminal = true, uploadquota.Aborted
		record, err := db.ApplyUploadQuota(context.Background(), change)
		if err != nil || !record.StagingReleased {
			t.Fatal("trusted cleanup was blocked by missing request policy", err)
		}
	}
}

func TestSEC030RequestPolicyRedisProjectionAndCorruptType(t *testing.T) {
	t.Run("invalid-integer", func(t *testing.T) {
		db, change, request := sec030RequestPolicyFixture(t, uploadquota.Reserve)
		conn := db.pool.Get()
		defer conn.Close()
		sec030RedisDo(t, conn, "HSET", db.dbPrefix+prefixFileRequests+request.Id, "maxfiles", "1.5")
		sec030RequestPolicyReject(t, db, change, uploadquota.ErrInvalid)
	})
	db, change, request := sec030RequestPolicyFixture(t, uploadquota.Reserve)
	conn := db.pool.Get()
	defer conn.Close()
	sec030RedisDo(t, conn, "HSET", db.dbPrefix+prefixFileRequests+request.Id, "name", strings.Repeat("n", 1048576), "apikey", strings.Repeat("p", 1048576))
	if _, err := db.ApplyUploadQuota(context.Background(), change); err != nil {
		t.Fatal("policy projection depended on large display or capability fields", err)
	}
}

func TestSEC030RequestPolicyRedisWatchedPolicyChange(t *testing.T) {
	s := sec030RedisNew(t)
	request := models.FileRequest{Id: "request", UserId: 1, MaxFiles: 10, MaxSize: 8, Expiry: 4600}
	s.db.SaveFileRequest(request)
	change := sec030RedisChange("policy-race", 1)
	change.CheckRequestPolicy = true
	before := sec030RedisRead(t, s.db)
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	target := s.config.RedisPrefix + prefixFileRequests + request.Id
	var wrapped *sec030RedisTypeRaceConn
	pool := &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
		conn, err := redigo.DialContext(ctx, "tcp", s.config.HostUrl)
		if err != nil {
			return nil, err
		}
		wrapped = &sec030RedisTypeRaceConn{Conn: conn, target: target, beforeMulti: func() {
			sec030RedisDo(t, s.conn, "HSET", target, "userid", 2)
		}}
		return wrapped, nil
	}}
	t.Cleanup(func() { _ = pool.Close() })
	contender := DatabaseProvider{pool: pool, dbPrefix: s.config.RedisPrefix}
	_, err := contender.ApplyUploadQuota(context.Background(), change)
	if !errors.Is(err, uploadquota.ErrConflict) || wrapped == nil || !wrapped.watchedTarget || !wrapped.injected || wrapped.executions != 1 {
		t.Errorf("policy change was not detected through WATCH/retry: %v", err)
	}
	if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) || len(s.db.GetAllMetadata()) != 0 {
		t.Fatal("policy race partially committed quota state or metadata")
	}
}
