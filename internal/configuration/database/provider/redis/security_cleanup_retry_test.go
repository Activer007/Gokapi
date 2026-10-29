//go:build test && security_cleanup

package redis

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// Keep the context-aware path used by production while injecting only at EXEC.
// The hook uses a separate real Redis connection, so WATCH observes a genuine
// concurrent revision/row change rather than a fabricated EXEC reply.
type sec040CleanupRetryConn struct {
	redigo.Conn
	execs      *atomic.Int32
	beforeExec func(int32) error
	unknown    error
}

var _ redigo.ConnWithContext = (*sec040CleanupRetryConn)(nil)

func (c *sec040CleanupRetryConn) DoContext(ctx context.Context, command string, args ...any) (any, error) {
	if !strings.EqualFold(command, "EXEC") {
		return redigo.DoContext(c.Conn, ctx, command, args...)
	}
	n := c.execs.Add(1)
	if err := c.beforeExec(n); err != nil {
		return nil, err
	}
	reply, err := redigo.DoContext(c.Conn, ctx, command, args...)
	if n == 1 && c.unknown != nil && err == nil && reply != nil {
		// Execute the transaction first: an unknown outcome must not be treated
		// as a known WATCH abort or as evidence that the row was preserved.
		return nil, c.unknown
	}
	return reply, err
}

func (c *sec040CleanupRetryConn) ReceiveContext(ctx context.Context) (any, error) {
	return redigo.ReceiveContext(c.Conn, ctx)
}

func TestSEC040CleanupRedisRetirementRetry(t *testing.T) {
	for _, scenario := range []string{"one-watch-abort", "repeated-watch-abort", "owner-changed", "unknown-committed-exec"} {
		t.Run(scenario, func(t *testing.T) {
			s := sec030RedisNew(t)
			ctx := context.Background()
			file := models.File{Id: "retry-file", UserId: 1, SHA1: "retry-blob", SizeBytes: 3,
				HotlinkId: "retry-hotlink", ExpireAt: 1, UnlimitedDownloads: true}
			s.db.SaveMetaData(file)
			s.db.SaveHotlink(file)
			if got, found, err := s.db.ReadRecoveryFile(ctx, file.Id); err != nil || !found || !reflect.DeepEqual(got, file) {
				t.Fatal("valid retirement fixture was not persisted", err)
			}

			var execs atomic.Int32
			unknown := errors.New("synthetic EXEC transport outcome unknown")
			beforeExec := func(n int32) error {
				if scenario == "unknown-committed-exec" || (n > 1 && scenario != "repeated-watch-abort") {
					return nil
				}
				if scenario == "owner-changed" {
					if _, err := s.conn.Do("HSET", s.config.RedisPrefix+prefixMetaData+file.Id, "UserId", 2); err != nil {
						return err
					}
				}
				_, err := s.conn.Do("SET", s.config.RedisPrefix+keyMetadataRevision, n)
				return err
			}
			p := s.db
			dial := s.db.pool.DialContext
			p.pool = &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
				conn, err := dial(ctx)
				if err != nil {
					return nil, err
				}
				wrapped := &sec040CleanupRetryConn{Conn: conn, execs: &execs, beforeExec: beforeExec}
				if scenario == "unknown-committed-exec" {
					wrapped.unknown = unknown
				}
				return wrapped, nil
			}}
			t.Cleanup(func() { _ = p.pool.Close() })

			retired, err := p.RetireCleanupFile(ctx, file, 1000, false)
			var wantExecs int32 = 1
			switch scenario {
			case "one-watch-abort":
				wantExecs = 2
				if err != nil || !retired {
					t.Errorf("known revision-only WATCH abort did not retire on retry: retired=%v err=%v", retired, err)
				}
			case "repeated-watch-abort":
				wantExecs = 2
				if !errors.Is(err, uploadquota.ErrConflict) || retired {
					t.Errorf("repeated WATCH abort did not stop with conflict: retired=%v err=%v", retired, err)
				}
			case "owner-changed":
				if !errors.Is(err, uploadquota.ErrConflict) || retired {
					t.Errorf("retry accepted a different owner: retired=%v err=%v", retired, err)
				}
			case "unknown-committed-exec":
				if !errors.Is(err, unknown) || retired {
					t.Errorf("unknown EXEC was reported as confirmed retirement: retired=%v err=%v", retired, err)
				}
			}
			if got := execs.Load(); got != wantExecs {
				t.Errorf("EXEC count=%d; want %d", got, wantExecs)
			}
			got, found, readErr := s.db.ReadRecoveryFile(ctx, file.Id)
			if readErr != nil {
				t.Fatal("could not inspect final retirement state", readErr)
			}
			target, linked := s.db.GetHotlink(file.HotlinkId)
			if scenario == "one-watch-abort" || scenario == "unknown-committed-exec" {
				if found || linked {
					t.Error("successful underlying EXEC did not remove metadata and its owned hotlink")
				}
			} else {
				want := file
				if scenario == "owner-changed" {
					want.UserId = 2
				}
				if !found || !reflect.DeepEqual(got, want) || !linked || target != file.Id {
					t.Error("rejected retirement changed the current row or its hotlink")
				}
			}
		})
	}
}
