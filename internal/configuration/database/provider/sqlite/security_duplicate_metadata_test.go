//go:build test && security_duplicate

package sqlite

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

func TestSEC030DuplicateSQLiteSourceConflictAtomic(t *testing.T) {
	for _, scenario := range []string{"unchanged", "deleted", "transferred", "blob-changed", "encryption-changed", "pending", "expired", "exhausted"} {
		t.Run(scenario, func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			source := models.File{
				Id: "duplicate-source", UserId: 1, Name: "synthetic.bin", SizeBytes: 3, SHA1: "synthetic-blob",
				UnlimitedTime: true, UnlimitedDownloads: true,
				Encryption: models.EncryptionInfo{IsEncrypted: true, DecryptionKey: bytes.Repeat([]byte{1}, 32), Nonce: []byte{2, 3}},
			}
			p.SaveMetaData(source)
			target := source
			target.Id, target.UserId, target.HotlinkId = "duplicate-target", 2, "duplicate-hotlink"
			change := sqliteQuotaReserve()
			change.Action, change.Source, change.File = uploadquota.DuplicateMetadata, &source, &target
			change.Upload = uploadquota.Reservation{ID: target.Id, UserID: target.UserId, BytesKnown: true, Bytes: target.SizeBytes}
			current := source
			switch scenario {
			case "deleted":
				p.DeleteMetaData(source.Id)
			case "transferred":
				current.UserId = 3
			case "blob-changed":
				current.SHA1 = "replacement-blob"
			case "encryption-changed":
				current.Encryption.Nonce = []byte{4, 5}
			case "pending":
				current.PendingDeletion = change.Now
			case "expired":
				current.UnlimitedTime, current.ExpireAt = false, change.Now-1
			case "exhausted":
				current.UnlimitedDownloads, current.DownloadsRemaining = false, 0
			}
			if scenario != "deleted" {
				p.SaveMetaData(current)
			}
			before, err := p.ReadUploadQuota(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.ApplyUploadQuota(ctx, change)
			if scenario == "unchanged" {
				if err != nil {
					t.Fatal("valid cross-owner duplicate rejected", err)
				}
				if got, ok := p.GetMetaDataById(target.Id); !ok || !reflect.DeepEqual(got, target) {
					t.Fatal("valid duplicate metadata was not committed")
				}
				if got, ok := p.GetHotlink(target.HotlinkId); !ok || got != target.Id {
					t.Fatal("valid duplicate hotlink was not committed")
				}
			} else {
				if !errors.Is(err, uploadquota.ErrConflict) {
					t.Fatalf("stale source = %v; want conflict", err)
				}
				if _, ok := p.GetMetaDataById(target.Id); ok {
					t.Fatal("conflicting duplicate partially committed metadata")
				}
				if _, ok := p.GetHotlink(target.HotlinkId); ok {
					t.Fatal("conflicting duplicate partially committed hotlink")
				}
			}
			after, err := p.ReadUploadQuota(ctx)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("metadata duplication changed the reservation ledger", err)
			}
		})
	}
}
