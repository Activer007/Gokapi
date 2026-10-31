//go:build test

package redis

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

func TestReadUserUploadUsageRedisLogicalContract(t *testing.T) {
	s := sec030RedisNew(t)
	ctx := t.Context()
	first := models.File{Id: "ordinary-one", UserId: 1, SHA1: "shared-blob", SizeBytes: 7, ExpireAt: 1}
	second := first
	second.Id = "ordinary-two"
	pending := models.File{Id: "pending", UserId: 1, SHA1: "pending-blob", SizeBytes: 5, PendingDeletion: 1}
	other := models.File{Id: "other-owner", UserId: 2, SHA1: "shared-blob", SizeBytes: 100}
	for _, file := range []models.File{first, second, pending, other} {
		s.db.SaveMetaData(file)
	}
	reservation := sec030RedisChange("active", 3)
	reservation.Limits.Instance.Bytes = 1000
	if _, err := s.db.ApplyUploadQuota(ctx, reservation); err != nil {
		t.Fatal(err)
	}
	beforeLedger := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)
	beforeRevision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	beforeFiles := s.db.GetAllMetadata()
	for owner, want := range map[int]uploadquota.Usage{1: {Bytes: 19, Files: 3}, 2: {Bytes: 100, Files: 1}, 3: {}} {
		if got, err := s.db.ReadUserUploadUsage(ctx, owner); err != nil || got != want {
			t.Fatalf("owner %d usage=%+v want=%+v err=%v", owner, got, want, err)
		}
	}
	if !reflect.DeepEqual(beforeLedger, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)) ||
		!reflect.DeepEqual(beforeRevision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) ||
		!reflect.DeepEqual(beforeFiles, s.db.GetAllMetadata()) {
		t.Fatal("read changed metadata, reservations or the revision")
	}
	if retired, err := s.db.RetireCleanupFile(ctx, pending, 1000, true); err != nil || !retired {
		t.Fatal("retirement failed", err)
	}
	if got, err := s.db.ReadUserUploadUsage(ctx, 1); err != nil || got != (uploadquota.Usage{Bytes: 14, Files: 2}) {
		t.Fatal("usage was not released only after retirement", got, err)
	}
}

func TestReadUserUploadUsageRedisUnavailable(t *testing.T) {
	s := sec030RedisNew(t)
	ctx := t.Context()
	s.db.SaveMetaData(models.File{Id: "invalid-size", UserId: 1, SizeBytes: -1})
	if got, err := s.db.ReadUserUploadUsage(ctx, 1); err == nil || got != (uploadquota.Usage{}) {
		t.Fatal("invalid metadata accepted", got, err)
	}
	s.db.DeleteMetaData("invalid-size")
	sec030RedisDo(t, s.conn, "SET", s.config.RedisPrefix+keyUploadQuota, "broken")
	if got, err := s.db.ReadUserUploadUsage(ctx, 1); !errors.Is(err, uploadquota.ErrInvalid) || got != (uploadquota.Usage{}) {
		t.Fatal("bad ledger accepted", got, err)
	}
	sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyUploadQuota)
	if _, err := s.db.ReadUserUploadUsage(ctx, 1); !errors.Is(err, uploadquota.ErrSchema) {
		t.Fatal("missing ledger accepted", err)
	}
	if _, err := s.db.ReadUserUploadUsage(nil, 1); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("nil context accepted", err)
	}
	if _, err := s.db.ReadUserUploadUsage(ctx, 0); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("invalid owner accepted", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.db.ReadUserUploadUsage(cancelled, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read succeeded", err)
	}
	// A dedicated one-slot pool is configured before first use. Its held
	// connection makes the caller deadline the only way to finish this read.
	p := s.db
	p.pool = &redigo.Pool{MaxActive: 1, Wait: true, DialContext: s.db.pool.DialContext}
	t.Cleanup(func() { _ = p.pool.Close() })
	held, err := p.pool.GetContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	limited, finish := context.WithTimeout(ctx, 10*time.Millisecond)
	defer finish()
	if _, err := p.ReadUserUploadUsage(limited, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("connection wait ignored deadline", err)
	}
}

type usageReadConn struct {
	redigo.Conn
	execs      *atomic.Int32
	beforeExec func(int32) error
}

func (c *usageReadConn) DoContext(ctx context.Context, command string, args ...any) (any, error) {
	if strings.EqualFold(command, "EXEC") {
		if err := c.beforeExec(c.execs.Add(1)); err != nil {
			return nil, err
		}
	}
	return redigo.DoContext(c.Conn, ctx, command, args...)
}
func (c *usageReadConn) ReceiveContext(ctx context.Context) (any, error) {
	return redigo.ReceiveContext(c.Conn, ctx)
}

func usageReadPool(t *testing.T, s sec030RedisScene, execs *atomic.Int32, before func(int32) error) DatabaseProvider {
	t.Helper()
	p := s.db
	dial := s.db.pool.DialContext
	p.pool = &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
		conn, err := dial(ctx)
		if err != nil {
			return nil, err
		}
		return &usageReadConn{Conn: conn, execs: execs, beforeExec: before}, nil
	}}
	t.Cleanup(func() { _ = p.pool.Close() })
	return p
}

func TestReadUserUploadUsageRedisConcurrentCommitAndRetryBound(t *testing.T) {
	s := sec030RedisNew(t)
	ctx := t.Context()
	change := sec030RedisChange("in-flight", 3)
	if _, err := s.db.ApplyUploadQuota(ctx, change); err != nil {
		t.Fatal(err)
	}
	change.Action, change.FileID = uploadquota.Claim, "committed-file"
	if _, err := s.db.ApplyUploadQuota(ctx, change); err != nil {
		t.Fatal(err)
	}
	change.Action = uploadquota.Commit
	change.File = &models.File{Id: change.FileID, UserId: change.Upload.UserID, UploadRequestId: change.Upload.RequestID, SizeBytes: 3}
	var execs atomic.Int32
	p := usageReadPool(t, s, &execs, func(n int32) error {
		if n == 1 {
			_, err := s.db.ApplyUploadQuota(ctx, change)
			return err
		}
		return nil
	})
	if got, err := p.ReadUserUploadUsage(ctx, 1); err != nil || got != (uploadquota.Usage{Bytes: 3, Files: 1}) || execs.Load() != 2 {
		t.Fatal("concurrent commit was not retried as one consistent snapshot", got, execs.Load(), err)
	}
	execs.Store(0)
	p = usageReadPool(t, s, &execs, func(n int32) error {
		_, err := s.conn.Do("SET", s.config.RedisPrefix+keyMetadataRevision, n)
		return err
	})
	if got, err := p.ReadUserUploadUsage(ctx, 1); !errors.Is(err, uploadquota.ErrRetry) || got != (uploadquota.Usage{}) || execs.Load() != userUsageReadAttempts {
		t.Fatal("repeated conflicts were not bounded and unavailable", got, execs.Load(), err)
	}
}
