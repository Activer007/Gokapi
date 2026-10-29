//go:build test

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

func sec030RedisWholeHotlinkCommit(t *testing.T, db DatabaseProvider) uploadquota.Change {
	t.Helper()
	change := sec030RedisChange("whole-hotlink-upload", 3)
	change.Upload.RequestID = ""
	sec030RedisApply(t, db, change)
	change.Action, change.FileID = uploadquota.Claim, "whole-hotlink-file"
	sec030RedisApply(t, db, change)
	file := models.File{Id: change.FileID, UserId: change.Upload.UserID,
		SizeBytes: change.Upload.Bytes, SHA1: "synthetic-blob", HotlinkId: "whole-hotlink"}
	change.Action, change.File = uploadquota.Commit, &file
	return change
}

func TestSEC030WholeHotlinkRedisCommitAndReplay(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new-mapping"
		if existing {
			name = "existing-same-target"
		}
		t.Run(name, func(t *testing.T) {
			s := sec030RedisNew(t)
			commit := sec030RedisWholeHotlinkCommit(t, s.db)
			if existing {
				s.db.SaveHotlink(*commit.File)
			}
			sec030RedisApply(t, s.db, commit)
			if target, ok := s.db.GetHotlink(commit.File.HotlinkId); !ok || target != commit.File.Id {
				t.Fatal("committed metadata has no matching hotlink mapping")
			}
			before, files := sec030RedisRead(t, s.db), s.db.GetAllMetadata()
			s.db.DeleteHotlink(commit.File.HotlinkId)
			sec030RedisApply(t, s.db, commit)
			if _, ok := s.db.GetHotlink(commit.File.HotlinkId); ok {
				t.Fatal("committed replay resurrected a subsequently deleted hotlink")
			}
			if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(files, s.db.GetAllMetadata()) {
				t.Fatal("committed replay changed ledger or metadata")
			}
		})
	}
}

func TestSEC030WholeHotlinkRedisCollisionAtomic(t *testing.T) {
	for _, kind := range []string{"other-target", "wrong-type"} {
		t.Run(kind, func(t *testing.T) {
			s := sec030RedisNew(t)
			commit := sec030RedisWholeHotlinkCommit(t, s.db)
			key := s.config.RedisPrefix + prefixHotlinks + commit.File.HotlinkId
			if kind == "other-target" {
				sec030RedisDo(t, s.conn, "SET", key, "protected-other-file")
			} else {
				sec030RedisDo(t, s.conn, "LPUSH", key, "protected-list")
			}
			before, files := sec030RedisRead(t, s.db), s.db.GetAllMetadata()
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			if _, err := s.db.ApplyUploadQuota(context.Background(), commit); !errors.Is(err, uploadquota.ErrConflict) {
				t.Errorf("hotlink collision error=%v; want reservation conflict", err)
			}
			if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(files, s.db.GetAllMetadata()) ||
				!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("rejected hotlink collision partially committed metadata, ledger or revision")
			}
			if kind == "other-target" {
				if target, _ := redigo.String(s.conn.Do("GET", key)); target != "protected-other-file" {
					t.Fatal("hotlink collision overwrote the protected mapping")
				}
			} else if values, _ := redigo.Strings(s.conn.Do("LRANGE", key, 0, -1)); !reflect.DeepEqual(values, []string{"protected-list"}) {
				t.Fatal("hotlink collision changed the protected Redis type/value")
			}
		})
	}
}

func TestSEC030WholeHotlinkRedisWatchedRace(t *testing.T) {
	s := sec030RedisNew(t)
	commit := sec030RedisWholeHotlinkCommit(t, s.db)
	before, files := sec030RedisRead(t, s.db), s.db.GetAllMetadata()
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	key := s.config.RedisPrefix + prefixHotlinks + commit.File.HotlinkId
	var wrapped *sec030RedisTypeRaceConn
	checkedBeforeRace := false
	pool := &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
		conn, err := redigo.DialContext(ctx, "tcp", s.config.HostUrl)
		if err != nil {
			return nil, err
		}
		wrapped = &sec030RedisTypeRaceConn{Conn: conn, target: key, beforeMulti: func() {
			checkedBeforeRace = wrapped.watchedTarget && wrapped.checkedTarget
			sec030RedisDo(t, s.conn, "SET", key, "protected-race-file")
		}}
		return wrapped, nil
	}}
	t.Cleanup(func() { _ = pool.Close() })
	db := DatabaseProvider{pool: pool, dbPrefix: s.config.RedisPrefix}
	_, err := db.ApplyUploadQuota(context.Background(), commit)
	if !errors.Is(err, uploadquota.ErrConflict) || wrapped == nil || !wrapped.injected || !checkedBeforeRace || wrapped.executions != 1 {
		t.Errorf("hotlink race did not reject through WATCH/TYPE: %v", err)
	}
	if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(files, s.db.GetAllMetadata()) ||
		!reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
		t.Fatal("hotlink race partially committed metadata, ledger or revision")
	}
	if target, _ := redigo.String(s.conn.Do("GET", key)); target != "protected-race-file" {
		t.Fatal("hotlink race overwrote the external mapping")
	}
}
