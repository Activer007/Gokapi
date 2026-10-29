//go:build test

package redis

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// All records and Redis instances are synthetic. These tests exercise database
// transactions, not HTTP uploads, actual storage reservations or production data.
type sec030RedisScene struct {
	db     DatabaseProvider
	config models.DbConnection
	conn   redigo.Conn
}

func sec030RedisNew(t *testing.T) sec030RedisScene {
	t.Helper()
	config := models.DbConnection{HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "sec030_" + rand.Text() + "_"}
	db, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	conn := db.pool.Get()
	t.Cleanup(func() { _ = conn.Close() })
	return sec030RedisScene{db: db, config: config, conn: conn}
}

func sec030RedisDo(t *testing.T, conn redigo.Conn, command string, args ...any) any {
	t.Helper()
	reply, err := conn.Do(command, args...)
	if err != nil {
		t.Fatal(err)
	}
	return reply
}

func sec030RedisRead(t *testing.T, db DatabaseProvider) uploadquota.Ledger {
	t.Helper()
	ledger, err := db.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func sec030RedisChange(id string, n int64) uploadquota.Change {
	return uploadquota.Change{
		Action: uploadquota.Reserve, Now: 1000,
		Upload: uploadquota.Reservation{ID: id, UserID: 1, RequestID: "request", BytesKnown: true, Bytes: n, StagingBytes: n, ExpiresAt: 4600},
		Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 100, Files: 10}, Request: uploadquota.Usage{Bytes: 100, Files: 10}, Instance: uploadquota.Usage{Bytes: 100, Files: 10}, UserActive: 10, RequestActive: 10, InstanceActive: 10, StagingBytes: 100},
	}
}

func sec030RedisApply(t *testing.T, db DatabaseProvider, change uploadquota.Change) uploadquota.Reservation {
	t.Helper()
	r, err := db.ApplyUploadQuota(context.Background(), change)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSEC030RedisSchemaAndReopen(t *testing.T) {
	t.Run("new-and-active-upgrade-preserved", func(t *testing.T) {
		s := sec030RedisNew(t)
		if s.db.GetDbVersion() != 9 || len(sec030RedisRead(t, s.db).Records) != 0 {
			t.Fatal("new schema not ready")
		}
		file := models.File{Id: "existing", UserId: 1, SizeBytes: 2, UploadRequestId: "request"}
		s.db.SaveMetaData(file)
		sec030RedisApply(t, s.db, sec030RedisChange("active", 3))
		beforeLedger, beforeFiles := sec030RedisRead(t, s.db), s.db.GetAllMetadata()
		s.db.SetDbVersion(8)
		s.db.Upgrade(8)
		s.db.SetDbVersion(9)
		s.db.Upgrade(8) // Idempotent retry must not reset an existing ledger.
		other, err := New(s.config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(other.Close)
		if !reflect.DeepEqual(beforeLedger, sec030RedisRead(t, other)) || !reflect.DeepEqual(beforeFiles, other.GetAllMetadata()) {
			t.Fatal("upgrade/reopen changed active reservations or metadata")
		}
	})
	t.Run("old-schema-creates-empty-ledger", func(t *testing.T) {
		s := sec030RedisNew(t)
		s.db.SaveMetaData(models.File{Id: "legacy", UserId: 1, SizeBytes: 2})
		before := s.db.GetAllMetadata()
		sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyUploadQuota, s.config.RedisPrefix+keyMetadataRevision)
		s.db.SetDbVersion(8)
		if _, err := s.db.ReadUploadQuota(context.Background()); !errors.Is(err, uploadquota.ErrSchema) || !errors.Is(err, uploadquota.ErrNotFound) {
			t.Fatalf("old absent structures=%v", err)
		}
		s.db.Upgrade(8)
		s.db.SetDbVersion(9)
		if len(sec030RedisRead(t, s.db).Records) != 0 || !reflect.DeepEqual(before, s.db.GetAllMetadata()) {
			t.Fatal("legacy upgrade lost metadata or invented reservations")
		}
	})
	t.Run("new-schema-missing-ledger-refuses", func(t *testing.T) {
		s := sec030RedisNew(t)
		sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyUploadQuota)
		other, err := New(s.config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(other.Close)
		if _, err = other.ReadUploadQuota(context.Background()); !errors.Is(err, uploadquota.ErrSchema) || errors.Is(err, uploadquota.ErrNotFound) {
			t.Fatalf("missing ledger read=%v", err)
		}
		if _, err = other.ApplyUploadQuota(context.Background(), sec030RedisChange("missing", 1)); !errors.Is(err, uploadquota.ErrSchema) {
			t.Fatalf("missing ledger apply=%v", err)
		}
		if exists, _ := redigo.Int(s.conn.Do("EXISTS", s.config.RedisPrefix+keyUploadQuota)); exists != 0 {
			t.Fatal("missing ledger silently recreated")
		}
	})
	t.Run("active-ledger-missing-revision-is-not-absent", func(t *testing.T) {
		s := sec030RedisNew(t)
		sec030RedisApply(t, s.db, sec030RedisChange("retained", 1))
		before := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)
		s.db.SetDbVersion(8)
		sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyMetadataRevision)
		if _, err := s.db.ReadUploadQuota(context.Background()); !errors.Is(err, uploadquota.ErrSchema) || errors.Is(err, uploadquota.ErrNotFound) {
			t.Fatalf("existing ledger treated as absent: %v", err)
		}
		if !reflect.DeepEqual(before, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)) {
			t.Fatal("partial upgrade lost active ledger")
		}
	})
}

func TestSEC030RedisCorruptLedgerAndTypes(t *testing.T) {
	for name, data := range map[string][]byte{
		"invalid-json":    []byte("not-json"),
		"unknown-version": []byte(`{"Version":2,"Records":{}}`),
		"oversize":        bytes.Repeat([]byte(" "), uploadquota.MaxLedgerBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			s := sec030RedisNew(t)
			key := s.config.RedisPrefix + keyUploadQuota
			sec030RedisDo(t, s.conn, "SET", key, data)
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			if _, err := s.db.ReadUploadQuota(context.Background()); err == nil {
				t.Fatal("corrupt ledger read accepted")
			}
			if _, err := s.db.ApplyUploadQuota(context.Background(), sec030RedisChange("rejected", 1)); err == nil {
				t.Fatal("corrupt ledger mutation accepted")
			}
			stored, err := redigo.Bytes(s.conn.Do("GET", key))
			if err != nil || !bytes.Equal(stored, data) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("rejected operation changed corruption/revision")
			}
			if len(s.db.GetAllMetadata()) != 0 {
				t.Fatal("rejected operation created metadata")
			}
		})
	}
	for _, which := range []string{keyUploadQuota, keyMetadataRevision} {
		t.Run("wrong-type-"+which, func(t *testing.T) {
			s := sec030RedisNew(t)
			key := s.config.RedisPrefix + which
			sec030RedisDo(t, s.conn, "DEL", key)
			sec030RedisDo(t, s.conn, "LPUSH", key, "protected")
			if _, err := s.db.ApplyUploadQuota(context.Background(), sec030RedisChange("rejected", 1)); !errors.Is(err, uploadquota.ErrSchema) || errors.Is(err, uploadquota.ErrNotFound) {
				t.Fatalf("wrong type=%v", err)
			}
			stored, err := redigo.String(s.conn.Do("LINDEX", key, 0))
			if err != nil || stored != "protected" || len(s.db.GetAllMetadata()) != 0 {
				t.Fatal("wrong-type rejection mutated data")
			}
		})
	}
	t.Run("wrong-type-metadata", func(t *testing.T) {
		s := sec030RedisNew(t)
		key := s.config.RedisPrefix + prefixMetaData + "protected"
		sec030RedisDo(t, s.conn, "SET", key, "protected")
		before := sec030RedisRead(t, s.db)
		revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
		if _, err := s.db.ApplyUploadQuota(context.Background(), sec030RedisChange("rejected", 1)); err == nil {
			t.Fatal("wrong metadata type accepted")
		}
		if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
			t.Fatal("rejection changed ledger/revision")
		}
		if value, _ := redigo.String(s.conn.Do("GET", key)); value != "protected" {
			t.Fatal("protected wrong-type data overwritten")
		}
	})
}

func TestSEC030RedisIntegerPrecision(t *testing.T) {
	for _, n := range []int64{(1 << 53) + 1, math.MaxInt64} {
		t.Run(strconv.FormatInt(n, 10), func(t *testing.T) {
			s := sec030RedisNew(t)
			change := sec030RedisChange("exact", n)
			change.Limits.User.Bytes, change.Limits.Request.Bytes, change.Limits.Instance.Bytes, change.Limits.StagingBytes = n, n, n, n
			sec030RedisApply(t, s.db, change)
			claim := change
			claim.Action, claim.FileID = uploadquota.Claim, "exact-file"
			sec030RedisApply(t, s.db, claim)
			file := models.File{Id: "exact-file", UserId: 1, UploadRequestId: "request", SizeBytes: n, SHA1: "synthetic"}
			commit := change
			commit.Action, commit.File = uploadquota.Commit, &file
			sec030RedisApply(t, s.db, commit)
			ledger := sec030RedisRead(t, s.db)
			stored, ok := s.db.GetMetaDataById(file.Id)
			if !ok || stored.SizeBytes != n || ledger.Records["exact"].Bytes != n || ledger.Records["exact"].StagingBytes != n {
				t.Fatal("quota integers lost precision")
			}
		})
	}
}

func TestSEC030RedisCanceledContext(t *testing.T) {
	s := sec030RedisNew(t)
	sec030RedisApply(t, s.db, sec030RedisChange("existing", 1))
	before, files := sec030RedisRead(t, s.db), s.db.GetAllMetadata()
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.db.ApplyUploadQuota(ctx, sec030RedisChange("canceled", 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled apply=%v", err)
	}
	if _, err := s.db.ReadUploadQuota(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read=%v", err)
	}
	if err := s.db.ImportUploadQuota(ctx, uploadquota.EmptyLedger()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled import=%v", err)
	}
	if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(files, s.db.GetAllMetadata()) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
		t.Fatal("canceled operation changed state")
	}
}

func TestSEC030RedisRawMetadataInvalidatesWatch(t *testing.T) {
	for _, action := range []string{"save", "delete"} {
		t.Run(action, func(t *testing.T) {
			s := sec030RedisNew(t)
			peer, err := New(s.config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(peer.Close)
			file := models.File{Id: "protected", UserId: 1, SizeBytes: 3}
			s.db.SaveMetaData(file)
			ledger := sec030RedisRead(t, s.db)
			revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
			sec030RedisDo(t, s.conn, "WATCH", s.config.RedisPrefix+keyMetadataRevision)
			if action == "save" {
				file.SizeBytes = 4
				peer.SaveMetaData(file)
			} else {
				peer.DeleteMetaData(file.Id)
			}
			if reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
				t.Fatal("raw write kept revision")
			}
			sec030RedisDo(t, s.conn, "MULTI")
			sentinel := s.config.RedisPrefix + "must-not-commit"
			sec030RedisDo(t, s.conn, "SET", sentinel, "unsafe")
			reply := sec030RedisDo(t, s.conn, "EXEC")
			if reply != nil {
				t.Fatalf("raw mutation did not invalidate WATCH: %v", reply)
			}
			if exists, _ := redigo.Int(s.conn.Do("EXISTS", sentinel)); exists != 0 || !reflect.DeepEqual(ledger, sec030RedisRead(t, s.db)) {
				t.Fatal("aborted EXEC changed state")
			}
			got, exists := s.db.GetMetaDataById(file.Id)
			if (action == "save" && (!exists || got.SizeBytes != 4)) || (action == "delete" && exists) {
				t.Fatal("raw metadata mutation was lost")
			}
		})
	}
}

func TestSEC030RedisActualQuotaRetriesAfterRawWrite(t *testing.T) {
	s := sec030RedisNew(t)
	peer, err := New(s.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(peer.Close)
	change := sec030RedisChange("contender", 1)
	change.Limits.User.Bytes = 1
	protected := models.File{Id: "concurrent", UserId: 1, SizeBytes: 1, UploadRequestId: "request"}
	attempts := 0
	_, err = s.db.changeUploadQuota(context.Background(), func(conn redigo.Conn, ledger uploadquota.Ledger) (uploadquota.Ledger, uploadquota.Reservation, *models.File, error) {
		files, err := s.db.quotaMetadata(context.Background(), conn)
		if err != nil {
			return uploadquota.Ledger{}, uploadquota.Reservation{}, nil, err
		}
		attempts++
		if attempts == 1 {
			peer.SaveMetaData(protected)
		} // After snapshot, before EXEC.
		return uploadquota.Apply(ledger, files, change)
	})
	if !errors.Is(err, uploadquota.ErrQuota) || attempts != 2 {
		t.Fatalf("stale quota snapshot not retried: attempts=%d error=%v", attempts, err)
	}
	if len(sec030RedisRead(t, s.db).Records) != 0 || len(sec030RedisRead(t, peer).Records) != 0 {
		t.Fatal("rejected contender persisted")
	}
	for _, db := range []DatabaseProvider{s.db, peer} {
		file, ok := db.GetMetaDataById(protected.Id)
		if !ok || !reflect.DeepEqual(file, protected) || len(db.GetAllMetadata()) != 1 {
			t.Fatal("quota retry changed concurrent metadata")
		}
	}
}

func TestSEC030RedisRawWrongTypePreserved(t *testing.T) {
	s := sec030RedisNew(t)
	key := s.config.RedisPrefix + prefixMetaData + "protected"
	sec030RedisDo(t, s.conn, "SET", key, "protected")
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		s.db.SaveMetaData(models.File{Id: "protected", UserId: 1, SizeBytes: 10})
	}()
	if !panicked {
		t.Fatal("raw wrong-type metadata write did not fail")
	}
	if stored, _ := redigo.String(s.conn.Do("GET", key)); stored != "protected" {
		t.Fatal("failed raw write corrupted protected key")
	}
	if !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) || len(sec030RedisRead(t, s.db).Records) != 0 {
		t.Fatal("failed raw write changed quota state")
	}
}

func TestSEC030RedisExecRuntimeErrors(t *testing.T) {
	if err := checkQuotaExec([]any{"OK", int64(1)}, 2); err != nil {
		t.Fatal(err)
	}
	for _, reply := range []any{nil, []any{"OK"}, []any{"OK", redigo.Error("WRONGTYPE synthetic")}, []any{redigo.Error("OOM synthetic"), "OK"}} {
		if err := checkQuotaExec(reply, 2); err == nil {
			t.Fatalf("EXEC error accepted: %#v", reply)
		}
	}
	// Redis keeps successful commands in an EXEC with a runtime error; checking
	// every reply is required and cannot be described as automatic rollback.
	s := sec030RedisNew(t)
	bad, side := s.config.RedisPrefix+"wrong-type", s.config.RedisPrefix+"side-effect"
	sec030RedisDo(t, s.conn, "SET", bad, "protected")
	sec030RedisDo(t, s.conn, "MULTI")
	sec030RedisDo(t, s.conn, "SET", side, "retained")
	sec030RedisDo(t, s.conn, "HSET", bad, "field", "value")
	reply := sec030RedisDo(t, s.conn, "EXEC")
	if err := checkQuotaExec(reply, 2); err == nil {
		t.Fatal("actual EXEC runtime error was ignored")
	}
	if value, _ := redigo.String(s.conn.Do("GET", side)); value != "retained" {
		t.Fatal("fixture did not demonstrate EXEC partial failure")
	}
}

// This wrapper injects a nonfatal EXEC response while the actual isolated
// Redis connection remains inside MULTI, exercising cleanup and pool reuse.
type sec030RedisExecErrorConn struct {
	redigo.Conn
	injected  bool
	discarded bool
}

func (conn *sec030RedisExecErrorConn) DoContext(ctx context.Context, command string, args ...any) (any, error) {
	if command == "EXEC" && !conn.injected {
		conn.injected = true
		return nil, redigo.Error("ERR synthetic nonfatal EXEC failure")
	}
	if command == "DISCARD" {
		conn.discarded = true
	}
	return redigo.DoContext(conn.Conn, ctx, command, args...)
}

func (conn *sec030RedisExecErrorConn) ReceiveContext(ctx context.Context) (any, error) {
	return redigo.ReceiveContext(conn.Conn, ctx)
}

func TestSEC030RedisExecErrorDiscardsOpenTransaction(t *testing.T) {
	s := sec030RedisNew(t)
	before := sec030RedisRead(t, s.db)
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	var wrapped *sec030RedisExecErrorConn
	pool := &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
		conn, err := redigo.DialContext(ctx, "tcp", s.config.HostUrl)
		if err != nil {
			return nil, err
		}
		wrapped = &sec030RedisExecErrorConn{Conn: conn}
		return wrapped, nil
	}}
	t.Cleanup(func() { _ = pool.Close() })
	db := DatabaseProvider{pool: pool, dbPrefix: s.config.RedisPrefix}
	_, err := db.ApplyUploadQuota(context.Background(), sec030RedisChange("uncommitted", 1))
	if err == nil || wrapped == nil || !wrapped.injected || !wrapped.discarded {
		t.Fatalf("EXEC error did not discard transaction: %v", err)
	}
	borrowed, err := pool.GetContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer borrowed.Close()
	if pong, err := redigo.String(borrowed.Do("PING")); err != nil || pong != "PONG" {
		t.Fatalf("pooled connection retained MULTI: %q %v", pong, err)
	}
	if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) || len(s.db.GetAllMetadata()) != 0 {
		t.Fatal("failed EXEC leaked queued writes")
	}
}

type sec030RedisTypeRaceConn struct {
	redigo.Conn
	target        string
	beforeMulti   func()
	injected      bool
	watchedTarget bool
	checkedTarget bool
	executions    int
}

func (conn *sec030RedisTypeRaceConn) DoContext(ctx context.Context, command string, args ...any) (any, error) {
	if command == "WATCH" {
		for _, arg := range args {
			if arg == conn.target {
				conn.watchedTarget = true
			}
		}
	}
	if command == "MULTI" && !conn.injected {
		conn.injected = true
		conn.beforeMulti()
	}
	if command == "EXEC" {
		conn.executions++
	}
	reply, err := redigo.DoContext(conn.Conn, ctx, command, args...)
	if command == "TYPE" && len(args) == 1 && args[0] == conn.target && err == nil {
		kind, parseErr := redigo.String(reply, nil)
		conn.checkedTarget = parseErr == nil && kind == "none"
	}
	return reply, err
}

func (conn *sec030RedisTypeRaceConn) ReceiveContext(ctx context.Context) (any, error) {
	return redigo.ReceiveContext(conn.Conn, ctx)
}

func TestSEC030RedisTargetTypeRaceAbortsCommit(t *testing.T) {
	s := sec030RedisNew(t)
	existing := models.File{Id: "existing", UserId: 1, SizeBytes: 2}
	s.db.SaveMetaData(existing)
	change := sec030RedisChange("type-race", 1)
	sec030RedisApply(t, s.db, change)
	claim := change
	claim.Action, claim.FileID = uploadquota.Claim, "target"
	sec030RedisApply(t, s.db, claim)
	before := sec030RedisRead(t, s.db)
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	target := s.config.RedisPrefix + prefixMetaData + "target"
	var wrapped *sec030RedisTypeRaceConn
	pool := &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
		conn, err := redigo.DialContext(ctx, "tcp", s.config.HostUrl)
		if err != nil {
			return nil, err
		}
		wrapped = &sec030RedisTypeRaceConn{Conn: conn, target: target, beforeMulti: func() {
			// Simulate an old/external writer that does not update revision.
			sec030RedisDo(t, s.conn, "SET", target, "external-protected")
		}}
		return wrapped, nil
	}}
	t.Cleanup(func() { _ = pool.Close() })
	db := DatabaseProvider{pool: pool, dbPrefix: s.config.RedisPrefix}
	file := models.File{Id: "target", UserId: 1, UploadRequestId: "request", SizeBytes: 1, SHA1: "synthetic"}
	commit := change
	commit.Action, commit.File = uploadquota.Commit, &file
	_, err := db.ApplyUploadQuota(context.Background(), commit)
	if err == nil || wrapped == nil || !wrapped.injected || !wrapped.watchedTarget || !wrapped.checkedTarget || wrapped.executions != 1 {
		t.Fatalf("target type race not rejected through WATCH: %v", err)
	}
	if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
		t.Fatal("failed type-race commit changed ledger/revision")
	}
	if stored, _ := redigo.String(s.conn.Do("GET", target)); stored != "external-protected" {
		t.Fatal("type-race commit overwrote target")
	}
	if stored, ok := s.db.GetMetaDataById(existing.Id); !ok || !reflect.DeepEqual(stored, existing) {
		t.Fatal("type-race commit changed existing metadata")
	}
}

func TestSEC030RedisTrustedCleanupWithCorruptMetadata(t *testing.T) {
	for _, action := range []uploadquota.Action{uploadquota.Abort, uploadquota.ReleaseStaging} {
		t.Run(string(action), func(t *testing.T) {
			s := sec030RedisNew(t)
			change := sec030RedisChange("cleanup", 1)
			sec030RedisApply(t, s.db, change)
			file := models.File{Id: "committed", UserId: 1, UploadRequestId: "request", SizeBytes: 1}
			if action == uploadquota.ReleaseStaging {
				claim := change
				claim.Action, claim.FileID = uploadquota.Claim, file.Id
				sec030RedisApply(t, s.db, claim)
				commit := change
				commit.Action, commit.File = uploadquota.Commit, &file
				sec030RedisApply(t, s.db, commit)
			}
			key := s.config.RedisPrefix + prefixMetaData + "unrelated-corrupt"
			sec030RedisDo(t, s.conn, "SET", key, "protected")
			before := sec030RedisRead(t, s.db)
			if _, err := s.db.ApplyUploadQuota(context.Background(), sec030RedisChange("still-denied", 1)); err == nil {
				t.Fatal("new admission ignored corrupt metadata")
			}
			cleanup := change
			cleanup.Action, cleanup.Terminal = action, uploadquota.Aborted
			if _, err := s.db.ApplyUploadQuota(context.Background(), cleanup); err == nil {
				t.Fatal("cleanup accepted without confirmation")
			}
			if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) {
				t.Fatal("unconfirmed cleanup changed ledger")
			}
			cleanup.CleanupConfirmed = true
			got := sec030RedisApply(t, s.db, cleanup)
			if !got.StagingReleased || (action == uploadquota.Abort && got.State != uploadquota.Aborted) || (action == uploadquota.ReleaseStaging && got.State != uploadquota.Committed) {
				t.Fatal("trusted cleanup did not release staging")
			}
			if value, _ := redigo.String(s.conn.Do("GET", key)); value != "protected" {
				t.Fatal("cleanup changed unrelated corrupt metadata")
			}
			if action == uploadquota.ReleaseStaging {
				if got, ok := s.db.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
					t.Fatal("staging cleanup changed committed metadata")
				}
			}
		})
	}
}

type sec030RedisDeleteDuringReadConn struct {
	redigo.Conn
	beforeRange func()
	deleted     bool
}

func (conn *sec030RedisDeleteDuringReadConn) DoContext(ctx context.Context, command string, args ...any) (any, error) {
	if command == "GETRANGE" && !conn.deleted {
		conn.deleted = true
		conn.beforeRange()
	}
	return redigo.DoContext(conn.Conn, ctx, command, args...)
}

func (conn *sec030RedisDeleteDuringReadConn) ReceiveContext(ctx context.Context) (any, error) {
	return redigo.ReceiveContext(conn.Conn, ctx)
}

func TestSEC030RedisMissingLedgerDuringRead(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(strconv.FormatBool(both), func(t *testing.T) {
			s := sec030RedisNew(t)
			base := s.db.pool.Get()
			defer base.Close()
			wrapped := &sec030RedisDeleteDuringReadConn{Conn: base, beforeRange: func() {
				sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyUploadQuota)
				if both {
					sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyMetadataRevision)
				}
			}}
			if _, err := s.db.readUploadQuota(context.Background(), wrapped); !errors.Is(err, uploadquota.ErrSchema) || errors.Is(err, uploadquota.ErrNotFound) != both || !wrapped.deleted {
				t.Fatalf("disappearing ledger=%v", err)
			}
			if exists, _ := redigo.Int(s.conn.Do("EXISTS", s.config.RedisPrefix+keyUploadQuota)); exists != 0 {
				t.Fatal("missing ledger replaced with empty ledger")
			}
		})
	}
}

func TestSEC030RedisMigrationDestinationEmpty(t *testing.T) {
	t.Run("bootstrap-only", func(t *testing.T) {
		s := sec030RedisNew(t)
		before := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)
		revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
		if err := s.db.CheckEmptyForMigration(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
			t.Fatal("empty preflight changed bootstrap data")
		}
	})
	for _, suffix := range []string{prefixSessions + "orphan", idE2EInfo + "999", prefixStatisticsTraffic, prefixStatisticsTrafficSince, "unknown-application-data"} {
		t.Run(suffix, func(t *testing.T) {
			s := sec030RedisNew(t)
			key := s.config.RedisPrefix + suffix
			sec030RedisDo(t, s.conn, "SET", key, "protected")
			before := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)
			if err := s.db.CheckEmptyForMigration(context.Background()); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatalf("orphan destination accepted: %v", err)
			}
			if value, _ := redigo.String(s.conn.Do("GET", key)); value != "protected" {
				t.Fatal("preflight changed orphan data")
			}
			if !reflect.DeepEqual(before, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyUploadQuota)) {
				t.Fatal("preflight changed ledger")
			}
		})
	}
	t.Run("foreign-prefix", func(t *testing.T) {
		s := sec030RedisNew(t)
		key := "different-prefix:" + rand.Text()
		sec030RedisDo(t, s.conn, "SET", key, "foreign-protected")
		if err := s.db.CheckEmptyForMigration(context.Background()); err != nil {
			t.Fatal(err)
		}
		if value, _ := redigo.String(s.conn.Do("GET", key)); value != "foreign-protected" {
			t.Fatal("preflight changed foreign data")
		}
	})
	t.Run("literal-glob-prefix", func(t *testing.T) {
		config := models.DbConnection{HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "sec030_[literal]*?\\_"}
		db, err := New(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(db.Close)
		conn := db.pool.Get()
		defer conn.Close()
		foreign := "sec030_lZZZZ_orphan"
		sec030RedisDo(t, conn, "SET", foreign, "foreign-protected")
		if err := db.CheckEmptyForMigration(context.Background()); err != nil {
			t.Fatalf("literal prefix matched unrelated data: %v", err)
		}
		key := config.RedisPrefix + "orphan"
		sec030RedisDo(t, conn, "SET", key, "local-protected")
		if err := db.CheckEmptyForMigration(context.Background()); !errors.Is(err, uploadquota.ErrConflict) {
			t.Fatalf("literal prefix missed local data: %v", err)
		}
		for key, want := range map[string]string{foreign: "foreign-protected", key: "local-protected"} {
			if got, _ := redigo.String(conn.Do("GET", key)); got != want {
				t.Fatal("prefix check changed protected data")
			}
		}
	})
	t.Run("active-ledger-and-canceled-context", func(t *testing.T) {
		s := sec030RedisNew(t)
		sec030RedisApply(t, s.db, sec030RedisChange("active", 1))
		before := sec030RedisRead(t, s.db)
		if err := s.db.CheckEmptyForMigration(context.Background()); !errors.Is(err, uploadquota.ErrConflict) {
			t.Fatalf("active destination=%v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.db.CheckEmptyForMigration(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled preflight=%v", err)
		}
		if !reflect.DeepEqual(before, sec030RedisRead(t, s.db)) {
			t.Fatal("preflight changed active ledger")
		}
	})
}
