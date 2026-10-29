//go:build test && security_cleanup

package sqlite

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestSEC040CleanupSQLiteMetadataContract(t *testing.T) {
	t.Run("complete-locator-scan", func(t *testing.T) {
		p := newQuotaDatabase(t)
		ctx := context.Background()
		file := models.File{Id: "cleanup-a", UserId: 1, SHA1: "shared-blob", AwsBucket: "bucket-a", SizeBytes: 3}
		other := file
		other.Id, other.UserId, other.Name = "cleanup-b", 2, strings.Repeat("n", (64<<10)+1)
		p.SaveMetaData(file)
		p.SaveMetaData(other)
		ids, err := p.ReadCleanupIDs(ctx)
		if err != nil || !reflect.DeepEqual(ids, []string{file.Id, other.Id}) {
			t.Fatal("cleanup IDs were incomplete or unordered", err)
		}
		if referenced, err := p.HasBlobReference(ctx, file); err != nil || !referenced {
			t.Fatal("cross-owner reference was lost or display fields were decoded", err)
		}
		other.AwsBucket = "bucket-b"
		p.SaveMetaData(other)
		if referenced, err := p.HasBlobReference(ctx, file); err != nil || referenced {
			t.Fatal("a different bucket was counted as the same blob", err)
		}
		if _, err := p.sqliteDb.Exec("UPDATE FileMetaData SET SHA1 = ? WHERE Id = ?", strings.Repeat("x", 129), other.Id); err != nil {
			t.Fatal(err)
		}
		if _, err := p.HasBlobReference(ctx, file); err == nil {
			t.Fatal("invalid reference projection was accepted")
		}
	})
	t.Run("conditional-retirement", func(t *testing.T) {
		for _, scenario := range []string{"expired", "active", "confirmed-missing", "changed-owner", "hotlink-reassigned"} {
			t.Run(scenario, func(t *testing.T) {
				p := newQuotaDatabase(t)
				ctx := context.Background()
				file := models.File{Id: "cleanup-file", UserId: 1, SHA1: "synthetic-blob", SizeBytes: 3, HotlinkId: "cleanup-link", ExpireAt: 1, UnlimitedDownloads: true}
				if scenario == "active" || scenario == "confirmed-missing" {
					file.UnlimitedTime = true
				}
				p.SaveMetaData(file)
				p.SaveHotlink(file)
				current := file
				wantRetired := scenario != "active" && scenario != "changed-owner"
				if scenario == "changed-owner" {
					current.UserId = 2
					p.SaveMetaData(current)
				}
				if scenario == "hotlink-reassigned" {
					p.SaveHotlink(models.File{Id: "other-file", HotlinkId: file.HotlinkId})
				}
				retired, err := p.RetireCleanupFile(ctx, file, 1000, scenario == "confirmed-missing")
				if retired != wantRetired || (wantRetired && err != nil) || (!wantRetired && !errors.Is(err, uploadquota.ErrConflict)) {
					t.Fatal("unexpected retirement result", err)
				}
				got, found := p.GetMetaDataById(file.Id)
				if found == wantRetired || (found && !reflect.DeepEqual(got, current)) {
					t.Fatal("retirement removed or changed a conflicting row")
				}
				hotlink, exists := p.GetHotlink(file.HotlinkId)
				if scenario == "hotlink-reassigned" {
					if !exists || hotlink != "other-file" {
						t.Fatal("retirement deleted another file's hotlink")
					}
				} else if exists == wantRetired || (exists && hotlink != file.Id) {
					t.Fatal("metadata/hotlink retirement was not atomic")
				}
				if wantRetired {
					if removed, err := p.RetireCleanupFile(ctx, file, 1000, true); err != nil || removed {
						t.Fatal("missing row retirement was not idempotent", err)
					}
				}
			})
		}
	})
	t.Run("errors-retain-anchor", func(t *testing.T) {
		p := newQuotaDatabase(t)
		file := models.File{Id: "cleanup-file", UserId: 1, SHA1: "synthetic-blob", HotlinkId: "cleanup-link", UnlimitedDownloads: true}
		p.SaveMetaData(file)
		p.SaveHotlink(file)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := p.ReadCleanupIDs(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled ID scan was accepted", err)
		}
		if _, err := p.RetireCleanupFile(ctx, file, 1000, true); !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled retirement was accepted", err)
		}
		if _, err := p.sqliteDb.Exec("UPDATE FileMetaData SET Encryption = ? WHERE Id = ?", []byte("invalid-gob"), file.Id); err != nil {
			t.Fatal(err)
		}
		if retired, err := p.RetireCleanupFile(context.Background(), file, 1000, true); err == nil || retired {
			t.Fatal("corrupt metadata was retired")
		}
		var count int
		if err := p.sqliteDb.QueryRow("SELECT count(*) FROM FileMetaData WHERE Id = ?", file.Id).Scan(&count); err != nil || count != 1 {
			t.Fatal("read error lost the management anchor", err)
		}
		if id, found := p.GetHotlink(file.HotlinkId); !found || id != file.Id {
			t.Fatal("read error lost the hotlink anchor")
		}
	})
}

// A complete reference scan must reject ambiguous local names even when the
// inspected candidate is canonical. Remote object keys retain exact identity.
func TestSEC040CleanupSQLiteLocatorIdentity(t *testing.T) {
	t.Run("local-alias", func(t *testing.T) {
		p := newQuotaDatabase(t)
		file := models.File{Id: "locator-target", UserId: 1, SHA1: "shared-blob", SizeBytes: 3}
		other := models.File{Id: "locator-other", UserId: 2, SHA1: "./shared-blob", SizeBytes: 3}
		p.SaveMetaData(file)
		p.SaveMetaData(other)
		if referenced, err := p.HasBlobReference(context.Background(), file); referenced || !errors.Is(err, uploadquota.ErrInvalid) {
			t.Fatal("noncanonical local reference must fail closed instead of reporting an unreferenced blob", err)
		}
	})
	t.Run("case-only-local", func(t *testing.T) {
		p := newQuotaDatabase(t)
		file := models.File{Id: "locator-target", UserId: 1, SHA1: "shared-blob", SizeBytes: 3}
		other := models.File{Id: "locator-other", UserId: 2, SHA1: "SHARED-BLOB", SizeBytes: 3}
		p.SaveMetaData(file)
		p.SaveMetaData(other)
		if referenced, err := p.HasBlobReference(context.Background(), file); referenced || !errors.Is(err, uploadquota.ErrInvalid) {
			t.Fatal("case-only local locators must retain both anchors for manual identity review", err)
		}
	})
	t.Run("s3-exact-key", func(t *testing.T) {
		p := newQuotaDatabase(t)
		file := models.File{Id: "locator-target", UserId: 1, SHA1: "folder/shared-blob", AwsBucket: "cleanup-bucket", SizeBytes: 3}
		other := file
		other.Id, other.UserId = "locator-other", 2
		p.SaveMetaData(file)
		p.SaveMetaData(other)
		if referenced, err := p.HasBlobReference(context.Background(), file); err != nil || !referenced {
			t.Fatal("slash-bearing S3 key lost its exact cross-owner reference", err)
		}
		other.SHA1 = "folder/./shared-blob"
		p.SaveMetaData(other)
		if referenced, err := p.HasBlobReference(context.Background(), file); err != nil || referenced {
			t.Fatal("distinct S3 key bytes were normalized into a local path alias", err)
		}
	})
}
