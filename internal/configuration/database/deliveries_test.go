//go:build test

package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/alicebob/miniredis/v2"
	redigo "github.com/gomodule/redigo/redis"
)

func deliveryConfig(t *testing.T, kind int) models.DbConnection {
	t.Helper()
	if kind == dbabstraction.TypeRedis {
		return models.DbConnection{Type: kind, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "delivery-test:"}
	}
	return models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "delivery.sqlite")}
}

func deliveryOpen(t *testing.T, config models.DbConnection) dbabstraction.Database {
	t.Helper()
	p, err := dbabstraction.GetNew(config)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func deliveryFixture(token string, owner int) models.Delivery {
	return models.Delivery{Token: strings.Repeat(token, 32), OwnerID: owner, Title: "资料交付", Note: "文件不复制", FileIDs: []string{"third", "first", "second"}, CreatedAt: 1000, ExpiresAt: 2000}
}

func TestDeliveryProviderContract(t *testing.T) {
	for _, kind := range []int{dbabstraction.TypeSqlite, dbabstraction.TypeRedis} {
		t.Run(map[int]string{0: "sqlite", 1: "redis"}[kind], func(t *testing.T) {
			ctx := context.Background()
			cfg := deliveryConfig(t, kind)
			p := deliveryOpen(t, cfg)
			t.Cleanup(func() { p.Close() })
			d := deliveryFixture("a", 1)
			if ok, err := p.InsertDelivery(ctx, d); err != nil || !ok {
				t.Fatalf("insert=%v %v", ok, err)
			}
			changed := d
			changed.Title = "must not replace"
			if ok, err := p.InsertDelivery(ctx, changed); err != nil || ok {
				t.Fatalf("duplicate insert=%v %v", ok, err)
			}
			other := deliveryFixture("b", 2)
			if ok, err := p.InsertDelivery(ctx, other); err != nil || !ok {
				t.Fatal("other owner's insert failed", err)
			}
			if got, err := p.ListDeliveries(ctx, d.OwnerID); err != nil || !reflect.DeepEqual(got, []models.Delivery{d}) {
				t.Fatalf("owner listing=%+v %v", got, err)
			}
			if got, ok, err := p.ReadDelivery(ctx, d.Token); err != nil || !ok || !reflect.DeepEqual(got, d) {
				t.Fatalf("record/order changed: %+v %v", got, err)
			}
			if ok, err := p.RevokeDelivery(ctx, d.Token, 2, 1500); err != nil || ok {
				t.Fatalf("cross-owner revoke=%v %v", ok, err)
			}
			if ok, err := p.RevokeDelivery(ctx, d.Token, d.OwnerID, 1500); err != nil || !ok {
				t.Fatal("owner revoke failed", err)
			}
			if ok, err := p.RevokeDelivery(ctx, d.Token, d.OwnerID, 1600); err != nil || !ok {
				t.Fatal("idempotent revoke failed", err)
			}
			d.RevokedAt = 1500
			p.Close()
			p = deliveryOpen(t, cfg)
			if got, ok, err := p.ReadDelivery(ctx, d.Token); err != nil || !ok || !reflect.DeepEqual(got, d) {
				t.Fatalf("reopen lost revoked delivery=%+v %v", got, err)
			}
			if err := p.CheckEmptyForMigration(ctx); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatalf("delivery-only destination accepted: %v", err)
			}
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if _, _, err := p.ReadDelivery(cancelled, d.Token); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled read succeeded", err)
			}
		})
	}
}

func TestDeliveryUpgradeAndMigration(t *testing.T) {
	for _, kind := range []int{dbabstraction.TypeSqlite, dbabstraction.TypeRedis} {
		t.Run(map[int]string{0: "sqlite-to-redis", 1: "redis-to-sqlite"}[kind], func(t *testing.T) {
			ctx := context.Background()
			cfg, target := deliveryConfig(t, kind), deliveryConfig(t, 1-kind)
			p := deliveryOpen(t, cfg)
			defer p.Close()
			file := models.File{Id: "legacy-file", UserId: 1, SHA1: "unchanged", SizeBytes: 3}
			p.SaveMetaData(file)
			oldVersion := 18
			if kind == dbabstraction.TypeSqlite {
				raw, err := sql.Open("sqlite", cfg.HostUrl)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := raw.Exec("DROP TABLE DeliveryLists"); err != nil {
					t.Fatal(err)
				}
				_ = raw.Close()
			} else {
				oldVersion = 10
				conn, err := redigo.Dial("tcp", cfg.HostUrl)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := conn.Do("DEL", cfg.RedisPrefix+"delivery:version"); err != nil {
					t.Fatal(err)
				}
				_ = conn.Close()
			}
			p.SetDbVersion(oldVersion)
			if _, err := p.ReadAllDeliveries(ctx); !errors.Is(err, uploadquota.ErrNotFound) {
				t.Fatal("absent pre-upgrade structure not reported", err)
			}
			p.Upgrade(oldVersion)
			p.Upgrade(oldVersion) // Retrying an interrupted upgrade preserves rows.
			p.SetDbVersion(p.GetSchemaVersion())
			if got, ok := p.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
				t.Fatal("upgrade changed existing file")
			}
			d := deliveryFixture("c", 1)
			d.RevokedAt = 1500
			if ok, err := p.InsertDelivery(ctx, d); err != nil || !ok {
				t.Fatal("insert failed", err)
			}
			p.Upgrade(oldVersion)
			if err := MigrateChecked(ctx, cfg, target); err != nil {
				t.Fatal("migration failed", err)
			}
			q := deliveryOpen(t, target)
			defer q.Close()
			if got, ok, err := q.ReadDelivery(ctx, d.Token); err != nil || !ok || !reflect.DeepEqual(got, d) {
				t.Fatal("migration lost delivery or revoke state", err)
			}
			if got, ok := q.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
				t.Fatal("migration lost source file")
			}
		})
	}
}
