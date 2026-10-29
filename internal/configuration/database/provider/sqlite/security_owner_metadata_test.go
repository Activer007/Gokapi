//go:build test && security_owner

package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
)

func TestSEC030OwnerSQLiteMetadataPatchAtomic(t *testing.T) {
	for _, scenario := range []string{"transfer", "same-owner", "target-missing", "source-owner-changed", "pending", "invalid-pending"} {
		t.Run(scenario, func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			p.SaveUser(models.User{Id: 1, Name: "Synthetic source owner"}, false)
			p.SaveUser(models.User{Id: 2, Name: "Synthetic target owner"}, false)
			source := models.File{
				Id: "owner-source", UserId: 1, Name: "original.bin", SizeBytes: 3,
				SHA1: "owner-blob", UploadRequestId: "owner-request", HotlinkId: "owner-hotlink",
				UnlimitedTime: true, UnlimitedDownloads: true,
			}
			p.SaveMetaData(source)
			p.SaveHotlink(source)
			change := uploadquota.Change{
				Action: uploadquota.TransferOwner, Source: &source, Now: 1000,
				Upload: uploadquota.Reservation{ID: source.Id, UserID: 2},
				Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 3, Files: 1}},
			}
			// Simulate a newer download and parameter save after authorization.
			current := source
			current.Name, current.DownloadCount = "current.bin", 7
			var wantErr error
			switch scenario {
			case "same-owner":
				change.Upload.UserID = source.UserId
				change.Limits.User = uploadquota.Usage{}
			case "target-missing":
				p.DeleteUser(change.Upload.UserID)
				wantErr = uploadquota.ErrConflict
			case "source-owner-changed":
				current.UserId = 3
				wantErr = uploadquota.ErrConflict
			case "pending":
				current.PendingDeletion = change.Now
				wantErr = uploadquota.ErrConflict
			case "invalid-pending":
				wantErr = uploadquota.ErrInvalid
			}
			p.SaveMetaData(current)
			if scenario == "invalid-pending" {
				if _, err := p.sqliteDb.Exec("UPDATE FileMetaData SET PendingDeletion = 'invalid' WHERE Id = ?", source.Id); err != nil {
					t.Fatal(err)
				}
			}
			before, err := p.ReadUploadQuota(ctx)
			if err != nil {
				t.Fatal(err)
			}
			record, err := p.ApplyUploadQuota(ctx, change)
			if !errors.Is(err, wantErr) || !reflect.DeepEqual(record, uploadquota.Reservation{}) {
				t.Fatalf("owner transfer = %v; want %v with no reservation", err, wantErr)
			}
			if wantErr == nil {
				current.UserId = change.Upload.UserID
			}
			if scenario == "invalid-pending" {
				var owner int
				var pending string
				if err := p.sqliteDb.QueryRow("SELECT UserId, PendingDeletion FROM FileMetaData WHERE Id = ?", source.Id).Scan(&owner, &pending); err != nil || owner != current.UserId || pending != "invalid" {
					t.Fatal("failed transfer changed corrupt source metadata", err)
				}
			} else if got, ok := p.GetMetaDataById(source.Id); !ok || !reflect.DeepEqual(got, current) {
				t.Fatal("owner transfer replaced current fields or changed a rejected source")
			}
			if got, ok := p.GetHotlink(source.HotlinkId); !ok || got != source.Id {
				t.Fatal("owner transfer changed the existing hotlink")
			}
			after, err := p.ReadUploadQuota(ctx)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("owner transfer changed the reservation ledger", err)
			}
		})
	}
}
