//go:build test

package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/Gokapi/internal/configuration/database/dbabstraction"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

func quotaMigrationConfig(t *testing.T, kind int) models.DbConnection {
	t.Helper()
	if kind == 1 {
		return models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "migration_"}
	}
	return models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "migration.sqlite")}
}

func quotaMigrationOpen(t *testing.T, cfg models.DbConnection) dbabstraction.Database {
	t.Helper()
	p, err := dbabstraction.GetNew(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func migrationReservation() uploadquota.Change {
	return uploadquota.Change{Action: uploadquota.Reserve, Now: 1000,
		Upload: uploadquota.Reservation{ID: "synthetic", UserID: 1, BytesKnown: true, Bytes: 3, StagingBytes: 3, ExpiresAt: 4600},
		Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 100, Files: 10}, Request: uploadquota.Usage{Bytes: 100, Files: 10}, Instance: uploadquota.Usage{Bytes: 100, Files: 10}, UserActive: 10, RequestActive: 10, InstanceActive: 10, StagingBytes: 100}}
}

func TestSEC030MigrationReceipts(t *testing.T) {
	for _, kind := range []int{0, 1} {
		t.Run(map[int]string{0: "sqlite-to-redis", 1: "redis-to-sqlite"}[kind], func(t *testing.T) {
			ctx := context.Background()
			source, target := quotaMigrationConfig(t, kind), quotaMigrationConfig(t, 1-kind)
			p := quotaMigrationOpen(t, source)
			file := models.File{Id: "retained", UserId: 1, SizeBytes: 7, SHA1: "synthetic-shared"}
			p.SaveMetaData(file)
			change := migrationReservation()
			if _, err := p.ApplyUploadQuota(ctx, change); err != nil {
				t.Fatal(err)
			}
			// Active reservations must be rejected before destination creation.
			if err := MigrateChecked(ctx, source, target); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatalf("active migration=%v", err)
			}
			change.Action, change.Terminal, change.CleanupConfirmed = uploadquota.Abort, uploadquota.Aborted, true
			if _, err := p.ApplyUploadQuota(ctx, change); err != nil {
				t.Fatal(err)
			}
			before, err := p.ReadUploadQuota(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := MigrateChecked(ctx, source, target); err != nil {
				t.Fatal(err)
			}
			q := quotaMigrationOpen(t, target)
			after, err := q.ReadUploadQuota(ctx)
			if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(p.GetAllMetadata(), q.GetAllMetadata()) {
				t.Fatalf("copy lost metadata/receipts: %v", err)
			}
			if err := MigrateChecked(ctx, source, target); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatalf("nonempty destination accepted: %v", err)
			}
			if got, _ := p.ReadUploadQuota(ctx); !reflect.DeepEqual(got, before) {
				t.Fatal("migration changed source")
			}
		})
	}
}

func TestSEC030MigrationInvalidAndLegacy(t *testing.T) {
	ctx := context.Background()
	t.Run("invalid-owner", func(t *testing.T) {
		source, target := quotaMigrationConfig(t, 0), quotaMigrationConfig(t, 1)
		p := quotaMigrationOpen(t, source)
		p.SaveMetaData(models.File{Id: "invalid-old-record", UserId: 0})
		if err := MigrateChecked(ctx, source, target); !errors.Is(err, uploadquota.ErrInvalid) {
			t.Fatalf("invalid source accepted: %v", err)
		}
		if len(p.GetAllMetadata()) != 1 {
			t.Fatal("invalid source altered")
		}
	})
	t.Run("legacy-sqlite-no-source-upgrade", func(t *testing.T) {
		source, target := quotaMigrationConfig(t, 0), quotaMigrationConfig(t, 1)
		p := quotaMigrationOpen(t, source)
		p.SaveMetaData(models.File{Id: "legacy", UserId: 1, SizeBytes: 2})
		raw, err := sql.Open("sqlite", source.HostUrl)
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		if _, err := raw.Exec("DROP TABLE UploadQuota; PRAGMA user_version=15"); err != nil {
			t.Fatal(err)
		}
		if err := MigrateChecked(ctx, source, target); err != nil {
			t.Fatal(err)
		}
		if p.GetDbVersion() != 15 {
			t.Fatal("migration silently upgraded source")
		}
		q := quotaMigrationOpen(t, target)
		if len(q.GetAllMetadata()) != 1 {
			t.Fatal("legacy metadata lost")
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(ctx)
		cancel()
		if err := MigrateChecked(ctx, models.DbConnection{}, models.DbConnection{}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled migration=%v", err)
		}
	})
	t.Run("legacy-redis-no-source-upgrade", func(t *testing.T) {
		source, target := quotaMigrationConfig(t, 1), quotaMigrationConfig(t, 0)
		p := quotaMigrationOpen(t, source)
		p.SaveMetaData(models.File{Id: "legacy", UserId: 1, SizeBytes: 2})
		conn, err := redigo.Dial("tcp", source.HostUrl)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.Do("DEL", source.RedisPrefix+"uploadquota:ledger", source.RedisPrefix+"uploadquota:metadata-revision"); err != nil {
			t.Fatal(err)
		}
		p.SetDbVersion(8)
		if err := MigrateChecked(ctx, source, target); err != nil {
			t.Fatal(err)
		}
		if p.GetDbVersion() != 8 {
			t.Fatal("migration silently upgraded Redis source")
		}
		if len(quotaMigrationOpen(t, target).GetAllMetadata()) != 1 {
			t.Fatal("legacy Redis data lost")
		}
	})
	t.Run("partial-sqlite-upgrade-with-active-ledger", func(t *testing.T) {
		source, target := quotaMigrationConfig(t, 0), quotaMigrationConfig(t, 1)
		p := quotaMigrationOpen(t, source)
		if _, err := p.ApplyUploadQuota(ctx, migrationReservation()); err != nil {
			t.Fatal(err)
		}
		p.SetDbVersion(15)
		if err := MigrateChecked(ctx, source, target); !errors.Is(err, uploadquota.ErrConflict) {
			t.Fatalf("partial upgrade lost reservation: %v", err)
		}
		ledger, err := p.ReadUploadQuota(ctx)
		if err != nil || len(ledger.Records) != 1 {
			t.Fatal("source reservation changed")
		}
	})
	t.Run("newer-source-version", func(t *testing.T) {
		source, target := quotaMigrationConfig(t, 0), quotaMigrationConfig(t, 1)
		p := quotaMigrationOpen(t, source)
		p.SetDbVersion(p.GetSchemaVersion() + 1)
		if err := MigrateChecked(ctx, source, target); !errors.Is(err, uploadquota.ErrSchema) {
			t.Fatalf("newer source accepted: %v", err)
		}
	})
}

func TestSEC030MigrationRejectsOrphanSession(t *testing.T) {
	for _, kind := range []int{0, 1} {
		t.Run(map[int]string{0: "sqlite-target", 1: "redis-target"}[kind], func(t *testing.T) {
			source, target := quotaMigrationConfig(t, 1-kind), quotaMigrationConfig(t, kind)
			p := quotaMigrationOpen(t, source)
			p.SaveUser(models.User{Id: 1, Name: "synthetic-owner", UserLevel: models.UserLevelUser}, false)
			q := quotaMigrationOpen(t, target)
			q.SaveSession("synthetic-orphan-session", models.Session{UserId: 1, ValidUntil: time.Now().Add(time.Hour).Unix()})
			if err := MigrateChecked(context.Background(), source, target); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatalf("orphan session destination accepted: %v", err)
			}
			if len(q.GetAllUsers()) != 0 {
				t.Fatal("rejected migration imported users")
			}
		})
	}
}

func TestSEC030SchemaRejectsNewerDatabase(t *testing.T) {
	p := quotaMigrationOpen(t, quotaMigrationConfig(t, 0))
	p.SetDbVersion(p.GetSchemaVersion() + 1)
	previous, previousExit := db, osExit
	db = p
	exit := 0
	osExit = func(code int) { exit = code }
	t.Cleanup(func() { db, osExit = previous, previousExit })
	Upgrade()
	if exit != 1 || p.GetDbVersion() != p.GetSchemaVersion()+1 {
		t.Fatal("newer database was not refused without mutation")
	}
}
