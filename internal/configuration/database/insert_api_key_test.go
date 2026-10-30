package database

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

func TestInsertApiKeyDoesNotReplaceConcurrentCredential(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "keys.sqlite")}
			if backend == "redis" {
				config = models.DbConnection{Type: dbabstraction.TypeRedis, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "insert_"}
			}
			stores := make([]dbabstraction.Database, 2)
			for i := range stores {
				var err error
				stores[i], err = dbabstraction.GetNew(config)
				if err != nil {
					t.Fatal(err)
				}
				defer stores[i].Close()
			}
			type result struct {
				key     models.ApiKey
				created bool
				err     error
			}
			results := make(chan result, 2)
			var start sync.WaitGroup
			start.Add(1)
			for i, store := range stores {
				go func(i int, store dbabstraction.Database) {
					start.Wait()
					key := models.ApiKey{Id: "same-credential", UserId: i + 1, UploadRequestId: "collection", Expiry: time.Now().Add(time.Hour).Unix()}
					created, err := store.InsertApiKey(key)
					results <- result{key, created, err}
				}(i, store)
			}
			start.Done()
			var winner models.ApiKey
			count := 0
			for range stores {
				result := <-results
				if result.err != nil {
					t.Fatal(result.err)
				}
				if result.created {
					count++
					winner = result.key
				}
			}
			if count != 1 {
				t.Fatalf("created %d credentials, want one", count)
			}
			if stored, found := stores[0].GetApiKey(winner.Id); !found || stored != winner {
				t.Fatalf("winning credential was replaced: found=%v key=%+v", found, stored)
			}
		})
	}
}
