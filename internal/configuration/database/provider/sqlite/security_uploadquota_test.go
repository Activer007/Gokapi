//go:build test

package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func newQuotaDatabase(t *testing.T) DatabaseProvider {
	t.Helper()
	provider, err := New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "quota.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Close)
	return provider
}

func sqliteQuotaReserve() uploadquota.Change {
	return uploadquota.Change{
		Action: uploadquota.Reserve,
		Upload: uploadquota.Reservation{ID: "synthetic-upload", UserID: 1, RequestID: "synthetic-request", BytesKnown: true, Bytes: 3, StagingBytes: 6, ExpiresAt: 4600},
		Now:    1000,
		Limits: uploadquota.Limits{
			User: uploadquota.Usage{Bytes: 100, Files: 10}, Request: uploadquota.Usage{Bytes: 100, Files: 10},
			Instance: uploadquota.Usage{Bytes: 1000, Files: 100}, UserActive: 10, RequestActive: 10, InstanceActive: 100, StagingBytes: 1000,
		},
	}
}

func TestSEC030SQLiteQuotaSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "upgrade.sqlite")
	p, err := New(models.DbConnection{HostUrl: path})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	if p.GetDbVersion() != 16 || p.GetSchemaVersion() != 16 {
		t.Fatal("new quota database is not schema 16")
	}
	ledger, err := p.ReadUploadQuota(ctx)
	if err != nil || !reflect.DeepEqual(ledger, uploadquota.EmptyLedger()) {
		t.Fatalf("fresh ledger = %#v, error = %v", ledger, err)
	}
	file := models.File{Id: "synthetic-file", UserId: 1, Name: "synthetic.bin", SHA1: "shared", SizeBytes: 7, UploadRequestId: "synthetic-request", Encryption: models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1, 2}}}
	request := models.FileRequest{Id: "synthetic-request", UserId: 1, ApiKey: "synthetic-key", MaxFiles: 5, MaxSize: 9}
	p.SaveMetaData(file)
	p.SaveFileRequest(request)
	// Removing only the new table recreates the exact schema-15 boundary; old
	// metadata/request rows retain their original representations.
	if _, err := p.sqliteDb.Exec("DROP TABLE UploadQuota"); err != nil {
		t.Fatal(err)
	}
	p.SetDbVersion(15)
	p.Close()
	p, err = New(models.DbConnection{HostUrl: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadUploadQuota(ctx); !errors.Is(err, uploadquota.ErrSchema) {
		t.Fatalf("schema-15 missing ledger = %v", err)
	}
	p.Upgrade(15)
	// The existing facade sets the version after successful provider migration.
	p.SetDbVersion(p.GetSchemaVersion())
	if _, err := p.ApplyUploadQuota(ctx, sqliteQuotaReserve()); err != nil {
		t.Fatal(err)
	}
	before, err := p.ReadUploadQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Upgrade(15)
	after, err := p.ReadUploadQuota(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("repeated migration overwrote the existing ledger", err)
	}
	p.Close()
	p, err = New(models.DbConnection{HostUrl: path})
	if err != nil {
		t.Fatal(err)
	}
	gotFile, ok := p.GetMetaDataById(file.Id)
	if !ok || !reflect.DeepEqual(gotFile, file) {
		t.Fatal("metadata changed across migration/reopen")
	}
	gotRequest, ok := p.GetFileRequest(request.Id)
	if !ok || !reflect.DeepEqual(gotRequest, request) {
		t.Fatal("File Request semantics changed across migration/reopen")
	}
	reopened, err := p.ReadUploadQuota(ctx)
	if err != nil || !reflect.DeepEqual(reopened, before) || p.GetDbVersion() != 16 {
		t.Fatal("migrated ledger did not survive reopen", err)
	}
}

func TestSEC030SQLiteQuotaInvalidStorage(t *testing.T) {
	for _, scenario := range []string{"missing-row", "missing-table", "malformed-columns", "empty", "corrupt", "oversize-blob", "oversize-text"} {
		t.Run(scenario, func(t *testing.T) {
			p := newQuotaDatabase(t)
			want := uploadquota.ErrInvalid
			var err error
			switch scenario {
			case "missing-row":
				_, err = p.sqliteDb.Exec("DELETE FROM UploadQuota WHERE Id = 1")
				want = uploadquota.ErrSchema
			case "missing-table":
				_, err = p.sqliteDb.Exec("DROP TABLE UploadQuota")
				want = uploadquota.ErrSchema
			case "malformed-columns":
				_, err = p.sqliteDb.Exec("DROP TABLE UploadQuota; CREATE TABLE UploadQuota (Id INTEGER PRIMARY KEY)")
				want = uploadquota.ErrSchema
			case "empty":
				_, err = p.sqliteDb.Exec("UPDATE UploadQuota SET State = ? WHERE Id = 1", []byte{})
			case "corrupt":
				_, err = p.sqliteDb.Exec("UPDATE UploadQuota SET State = ? WHERE Id = 1", []byte("{}"))
			case "oversize-blob":
				_, err = p.sqliteDb.Exec("UPDATE UploadQuota SET State = ? WHERE Id = 1", bytes.Repeat([]byte(" "), uploadquota.MaxLedgerBytes+1))
			case "oversize-text":
				// TEXT character count is below the limit but UTF-8 byte count
				// exceeds it, despite the column's declared BLOB affinity.
				_, err = p.sqliteDb.Exec("UPDATE UploadQuota SET State = ? WHERE Id = 1", strings.Repeat("界", uploadquota.MaxLedgerBytes/3+1))
			}
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := p.ReadUploadQuota(context.Background())
			if !errors.Is(readErr, want) {
				t.Fatalf("invalid ledger read = %v; want %v", readErr, want)
			}
			if errors.Is(readErr, uploadquota.ErrNotFound) != (scenario == "missing-table") {
				t.Fatalf("missing/corrupt ledger distinction lost: %v", readErr)
			}
			if _, err := p.ApplyUploadQuota(context.Background(), sqliteQuotaReserve()); !errors.Is(err, want) {
				t.Fatalf("invalid ledger mutation = %v; want %v", err, want)
			}
			if err := p.ImportUploadQuota(context.Background(), uploadquota.EmptyLedger()); !errors.Is(err, want) {
				t.Fatalf("invalid ledger import = %v; want %v", err, want)
			}
		})
	}
}

func TestSEC030SQLiteCleanupWithCorruptMetadata(t *testing.T) {
	for _, action := range []uploadquota.Action{uploadquota.Abort, uploadquota.ReleaseStaging} {
		t.Run(string(action), func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			change := sqliteQuotaReserve()
			if _, err := p.ApplyUploadQuota(ctx, change); err != nil {
				t.Fatal(err)
			}
			if action == uploadquota.ReleaseStaging {
				change.Action, change.FileID = uploadquota.Claim, "synthetic-committed"
				if _, err := p.ApplyUploadQuota(ctx, change); err != nil {
					t.Fatal(err)
				}
				change.Action = uploadquota.Commit
				change.File = &models.File{Id: change.FileID, UserId: change.Upload.UserID, UploadRequestId: change.Upload.RequestID, SizeBytes: change.Upload.Bytes}
				if _, err := p.ApplyUploadQuota(ctx, change); err != nil {
					t.Fatal(err)
				}
			}
			p.SaveMetaData(models.File{Id: "synthetic-unrelated", UserId: 2, SizeBytes: 1})
			if _, err := p.sqliteDb.Exec("UPDATE FileMetaData SET SizeBytes = 'invalid' WHERE Id = 'synthetic-unrelated'"); err != nil {
				t.Fatal(err)
			}
			admission := sqliteQuotaReserve()
			admission.Upload.ID = "synthetic-other"
			if _, err := p.ApplyUploadQuota(ctx, admission); err == nil {
				t.Fatal("new admission ignored corrupt metadata")
			}
			change.Action, change.Terminal, change.CleanupConfirmed = action, uploadquota.Aborted, true
			record, err := p.ApplyUploadQuota(ctx, change)
			if err != nil || !record.StagingReleased {
				t.Fatalf("cleanup was blocked by unrelated metadata: %v", err)
			}
			var stored string
			if err := p.sqliteDb.QueryRow("SELECT SizeBytes FROM FileMetaData WHERE Id = 'synthetic-unrelated'").Scan(&stored); err != nil || stored != "invalid" {
				t.Fatal("cleanup changed unrelated corrupt metadata", err)
			}
		})
	}
}

func TestSEC030SQLiteQuotaCanceledRollback(t *testing.T) {
	p := newQuotaDatabase(t)
	p.sqliteDb.SetMaxOpenConns(1)
	p.sqliteDb.SetMaxIdleConns(1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := models.File{Id: "synthetic-canceled", UserId: 1, SizeBytes: 3, Encryption: models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{3, 4}}}
	err := p.withUploadQuotaTransaction(ctx, func(conn *sql.Conn) error {
		if err := saveMetaData(ctx, conn, file); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled commit = %v", err)
	}
	if _, ok := p.GetMetaDataById(file.Id); ok {
		t.Fatal("canceled transaction persisted metadata")
	}
	if _, err := p.ReadUploadQuota(context.Background()); err != nil {
		t.Fatal("canceled transaction left the only connection unusable", err)
	}
	p.SaveMetaData(file)
	got, ok := p.GetMetaDataById(file.Id)
	if !ok || !reflect.DeepEqual(got, file) {
		t.Fatal("metadata helper changed the legacy save representation")
	}
	for _, action := range []func() error{
		func() error { _, err := p.ReadUploadQuota(ctx); return err },
		func() error { _, err := p.ApplyUploadQuota(ctx, sqliteQuotaReserve()); return err },
		func() error { return p.ImportUploadQuota(ctx, uploadquota.EmptyLedger()) },
	} {
		if err := action(); !errors.Is(err, context.Canceled) {
			t.Fatalf("already canceled request = %v", err)
		}
	}
}

func TestSEC030SQLiteQuotaBackupRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	config := models.DbConnection{HostUrl: filepath.Join(dir, "original.sqlite")}
	backupConfig := models.DbConnection{HostUrl: filepath.Join(dir, "before-upgrade.sqlite")}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	file := models.File{Id: "synthetic-file", UserId: 1, SizeBytes: 7, UploadRequestId: "synthetic-request", Encryption: models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{5, 6}}}
	request := models.FileRequest{Id: "synthetic-request", UserId: 1, ApiKey: "synthetic-key", MaxFiles: 5, MaxSize: 9}
	p.SaveMetaData(file)
	p.SaveFileRequest(request)
	if _, err := p.sqliteDb.ExecContext(ctx, "DROP TABLE UploadQuota"); err != nil {
		t.Fatal(err)
	}
	p.SetDbVersion(15)
	// VACUUM INTO creates a consistent SQLite snapshot, including WAL writes.
	// Both files are synthetic and remain inside the test's temporary directory.
	if _, err := p.sqliteDb.ExecContext(ctx, "VACUUM INTO ?", backupConfig.HostUrl); err != nil {
		t.Fatal(err)
	}
	p.Upgrade(15)
	p.SetDbVersion(16)
	if got, ok := p.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
		t.Fatal("upgrade changed backed-up metadata")
	}
	if got, ok := p.GetFileRequest(request.Id); !ok || !reflect.DeepEqual(got, request) {
		t.Fatal("upgrade changed backed-up File Request")
	}
	if _, err := p.ApplyUploadQuota(ctx, sqliteQuotaReserve()); err != nil {
		t.Fatal(err)
	}
	upgradedLedger, err := p.ReadUploadQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	p, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := New(backupConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backup.Close() })
	backup.Close()
	backup, err = New(backupConfig)
	if err != nil {
		t.Fatal(err)
	}
	if backup.GetDbVersion() != 15 {
		t.Fatal("opening the backup changed its schema version")
	}
	if got, ok := backup.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
		t.Fatal("backup reopen did not restore original metadata")
	}
	if got, ok := backup.GetFileRequest(request.Id); !ok || !reflect.DeepEqual(got, request) {
		t.Fatal("backup reopen did not restore original File Request")
	}
	if _, err := backup.ReadUploadQuota(ctx); !errors.Is(err, uploadquota.ErrSchema) || !errors.Is(err, uploadquota.ErrNotFound) {
		t.Fatalf("schema-15 backup unexpectedly has quota state: %v", err)
	}
	var tables int
	if err := backup.sqliteDb.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'UploadQuota'").Scan(&tables); err != nil || tables != 0 {
		t.Fatal("new reader created quota state in the backup", err)
	}
	current, err := p.ReadUploadQuota(ctx)
	if err != nil || !reflect.DeepEqual(current, upgradedLedger) || p.GetDbVersion() != 16 {
		t.Fatal("backup inspection altered the upgraded database", err)
	}
	if got, ok := p.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
		t.Fatal("backup inspection changed upgraded metadata")
	}
	if got, ok := p.GetFileRequest(request.Id); !ok || !reflect.DeepEqual(got, request) {
		t.Fatal("backup inspection changed upgraded File Request")
	}
}

func TestSEC030SQLiteMigrationEmptyPreflight(t *testing.T) {
	for _, scenario := range []string{"empty", "orphan-session", "orphan-e2e", "missing-statistics", "nonempty-ledger"} {
		t.Run(scenario, func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			session := models.Session{UserId: 9001, RenewAt: 1000, ValidUntil: 4600}
			info := models.E2EInfoEncrypted{Version: 1, Nonce: []byte{1, 2}, Content: []byte("synthetic-ciphertext")}
			switch scenario {
			case "orphan-session":
				p.SaveSession("synthetic-orphan", session)
			case "orphan-e2e":
				p.SaveEnd2EndInfo(info, 9001)
			case "missing-statistics":
				if _, err := p.sqliteDb.Exec("DROP TABLE Statistics"); err != nil {
					t.Fatal(err)
				}
			case "nonempty-ledger":
				if _, err := p.ApplyUploadQuota(ctx, sqliteQuotaReserve()); err != nil {
					t.Fatal(err)
				}
			}
			before, err := p.ReadUploadQuota(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = p.CheckEmptyForMigration(ctx)
			if scenario == "empty" {
				if err != nil {
					t.Fatal("empty destination rejected", err)
				}
			} else if scenario == "missing-statistics" {
				if err == nil {
					t.Fatal("incomplete destination schema accepted")
				}
			} else if !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatalf("nonempty destination error = %v", err)
			}
			after, readErr := p.ReadUploadQuota(ctx)
			if readErr != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("preflight changed quota state", readErr)
			}
			if scenario == "orphan-session" {
				if got, ok := p.GetSession("synthetic-orphan"); !ok || got != session {
					t.Fatal("preflight changed orphan session")
				}
			}
			if scenario == "orphan-e2e" && !reflect.DeepEqual(p.GetEnd2EndInfo(9001), info) {
				t.Fatal("preflight changed orphan E2E configuration")
			}
			if len(p.GetAllUsers()) != 0 || len(p.GetAllMetadata()) != 0 {
				t.Fatal("preflight created users or metadata")
			}
		})
	}
}
