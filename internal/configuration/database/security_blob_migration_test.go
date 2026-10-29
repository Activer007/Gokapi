//go:build test && security_reclaim

package database

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/blobreclaim"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestSEC041MigrationPreservesDetachedCandidates(t *testing.T) {
	for _, backend := range []int{0, 1} {
		t.Run(map[int]string{0: "sqlite-to-redis", 1: "redis-to-sqlite"}[backend], func(t *testing.T) {
			ctx := context.Background()
			source, target := quotaMigrationConfig(t, backend), quotaMigrationConfig(t, 1-backend)
			p := quotaMigrationOpen(t, source)
			first, err := blobreclaim.New("detached-old-content", "")
			if err != nil {
				t.Fatal(err)
			}
			second, err := blobreclaim.New("same-name", "synthetic-bucket")
			if err != nil {
				t.Fatal(err)
			}
			if err := p.ImportBlobCandidates(ctx, []blobreclaim.Candidate{first, second}); err != nil {
				t.Fatal(err)
			}
			file := models.File{Id: "surviving-share", UserId: 1, SizeBytes: 3, SHA1: "current-content", UnlimitedTime: true, UnlimitedDownloads: true}
			p.SaveMetaData(file)
			before, err := p.ReadBlobCandidates(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := MigrateChecked(ctx, source, target); err != nil {
				t.Fatal("offline migration failed", err)
			}
			q := quotaMigrationOpen(t, target)
			after, err := q.ReadBlobCandidates(ctx)
			if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(p.GetAllMetadata(), q.GetAllMetadata()) {
				t.Fatal("migration lost an anchor or changed its token", err)
			}
			if err := MigrateChecked(ctx, source, target); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatal("migration overwrote a populated destination", err)
			}
			if unchanged, err := p.ReadBlobCandidates(ctx); err != nil || !reflect.DeepEqual(unchanged, before) {
				t.Fatal("migration changed the source", err)
			}
		})
	}
}
