//go:build test

package sqlite

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestReadUserUploadUsageSQLiteLogicalContract(t *testing.T) {
	p := newQuotaDatabase(t)
	ctx := t.Context()
	first := models.File{Id: "ordinary-one", UserId: 1, SHA1: "shared-blob", SizeBytes: 7, ExpireAt: 1}
	second := first
	second.Id = "ordinary-two"
	pending := models.File{Id: "pending", UserId: 1, SHA1: "pending-blob", SizeBytes: 5, PendingDeletion: 1}
	other := models.File{Id: "other-owner", UserId: 2, SHA1: "shared-blob", SizeBytes: 100}
	for _, file := range []models.File{first, second, pending, other} {
		p.SaveMetaData(file)
	}
	// Active reservations are checked for health but are not metadata usage.
	if _, err := p.ApplyUploadQuota(ctx, sqliteQuotaReserve()); err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err := p.sqliteDb.QueryRow("SELECT State FROM UploadQuota WHERE Id = 1").Scan(&before); err != nil {
		t.Fatal(err)
	}
	metadata := p.GetAllMetadata()
	for owner, want := range map[int]uploadquota.Usage{1: {Bytes: 19, Files: 3}, 2: {Bytes: 100, Files: 1}, 3: {}} {
		got, err := p.ReadUserUploadUsage(ctx, owner)
		if err != nil || got != want {
			t.Fatalf("owner %d usage=%+v want=%+v err=%v", owner, got, want, err)
		}
	}
	var after []byte
	if err := p.sqliteDb.QueryRow("SELECT State FROM UploadQuota WHERE Id = 1").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !reflect.DeepEqual(metadata, p.GetAllMetadata()) {
		t.Fatal("read changed metadata, reservations or the persisted ledger")
	}
	if retired, err := p.RetireCleanupFile(ctx, pending, 1000, true); err != nil || !retired {
		t.Fatal("pending metadata retirement failed", err)
	}
	if got, err := p.ReadUserUploadUsage(ctx, 1); err != nil || got != (uploadquota.Usage{Bytes: 14, Files: 2}) {
		t.Fatal("usage was not released only after retirement", got, err)
	}
}

func TestReadUserUploadUsageSQLiteUnavailable(t *testing.T) {
	p := newQuotaDatabase(t)
	ctx := t.Context()
	p.SaveMetaData(models.File{Id: "invalid-size", UserId: 1, SizeBytes: -1})
	if got, err := p.ReadUserUploadUsage(ctx, 1); err == nil || got != (uploadquota.Usage{}) {
		t.Fatal("invalid metadata returned a usable partial value", got, err)
	}
	p.DeleteMetaData("invalid-size")
	if _, err := p.sqliteDb.Exec("UPDATE UploadQuota SET State = ?", []byte("broken")); err != nil {
		t.Fatal(err)
	}
	if got, err := p.ReadUserUploadUsage(ctx, 1); !errors.Is(err, uploadquota.ErrInvalid) || got != (uploadquota.Usage{}) {
		t.Fatal("broken ledger reported empty usage", got, err)
	}
	if _, err := p.sqliteDb.Exec("DROP TABLE UploadQuota"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadUserUploadUsage(ctx, 1); !errors.Is(err, uploadquota.ErrSchema) {
		t.Fatal("missing ledger did not fail closed", err)
	}
	if _, err := p.ReadUserUploadUsage(nil, 1); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("nil context accepted", err)
	}
	if _, err := p.ReadUserUploadUsage(ctx, 0); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("invalid owner accepted", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.ReadUserUploadUsage(cancelled, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read succeeded", err)
	}
	p.sqliteDb.SetMaxOpenConns(1)
	conn, err := p.sqliteDb.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	limited, finish := context.WithTimeout(ctx, 10*time.Millisecond)
	defer finish()
	if _, err := p.ReadUserUploadUsage(limited, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting for a read connection ignored its deadline", err)
	}
}

func TestReadUserUploadUsageSQLiteConsistentRead(t *testing.T) {
	p := newQuotaDatabase(t)
	ctx := t.Context()
	p.SaveMetaData(models.File{Id: "concurrent", UserId: 1, SizeBytes: 7})
	healthy, err := uploadquota.Encode(uploadquota.EmptyLedger())
	if err != nil {
		t.Fatal(err)
	}
	// A writer atomically switches between a healthy 7-byte state and an
	// unavailable 100-byte state. A split ledger/metadata read could report
	// the latter as healthy; a single read transaction must never do so.
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(started)
		for range 40 {
			for _, state := range []struct {
				ledger []byte
				size   int
			}{{[]byte("broken"), 100}, {healthy, 7}} {
				tx, err := p.sqliteDb.BeginTx(ctx, nil)
				if err != nil {
					finished <- err
					return
				}
				if _, err = tx.ExecContext(ctx, "UPDATE UploadQuota SET State = ?", state.ledger); err == nil {
					_, err = tx.ExecContext(ctx, "UPDATE FileMetaData SET SizeBytes = ? WHERE Id = 'concurrent'", state.size)
				}
				if err != nil {
					_ = tx.Rollback()
					finished <- err
					return
				}
				if err = tx.Commit(); err != nil {
					finished <- err
					return
				}
			}
		}
		finished <- nil
	}()
	<-started
	for range 40 {
		got, err := p.ReadUserUploadUsage(ctx, 1)
		if err == nil && got != (uploadquota.Usage{Bytes: 7, Files: 1}) {
			t.Errorf("mixed healthy ledger and unavailable metadata: %+v", got)
		}
		if err != nil && !errors.Is(err, uploadquota.ErrInvalid) {
			t.Errorf("unexpected read error: %v", err)
		}
	}
	if err := <-finished; err != nil {
		t.Fatal("concurrent writer failed", err)
	}
	if got, err := p.ReadUserUploadUsage(ctx, 1); err != nil || got != (uploadquota.Usage{Bytes: 7, Files: 1}) {
		t.Fatal("final healthy state was not readable", got, err)
	}
}
