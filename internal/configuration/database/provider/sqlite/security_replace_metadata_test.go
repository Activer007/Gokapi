//go:build test && security_replace

package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

func TestSEC030ReplaceSQLiteContentPatchAtomic(t *testing.T) {
	for _, scenario := range []string{"growth", "shrink-over-limit", "source-owner-changed", "target-owner-changed", "source-text-changed", "delete-source", "quota-reject-delete"} {
		t.Run(scenario, func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			target := models.File{
				Id: "replace-target", UserId: 1, Name: "original.bin", Size: "3 B", SizeBytes: 3,
				SHA1: "target-blob", ContentType: "text/plain", HotlinkId: "target-hotlink",
				ExpireAt: 2000, DownloadsRemaining: 10, PasswordHash: "initial-password",
			}
			source := models.File{
				Id: "replace-source", UserId: 2, Name: "replacement.bin", Size: "5 B", SizeBytes: 5,
				SHA1: "source-blob", ContentType: "application/octet-stream", AwsBucket: "source-bucket",
				UploadRequestId: "source-request", HotlinkId: "source-hotlink", UnlimitedTime: true, UnlimitedDownloads: true,
				Encryption: models.EncryptionInfo{IsEncrypted: true, DecryptionKey: []byte{1, 2, 3}, Nonce: []byte{4, 5}},
			}
			if scenario == "shrink-over-limit" {
				target.SizeBytes, target.Size = 7, "7 B"
			}
			p.SaveMetaData(target)
			p.SaveMetaData(source)
			p.SaveHotlink(target)
			p.SaveHotlink(source)
			change := uploadquota.Change{
				Action: uploadquota.ReplaceMetadata, Target: &target, Source: &source, Now: 1000,
				Upload: uploadquota.Reservation{ID: target.Id, UserID: target.UserId},
				Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 5}, Instance: uploadquota.Usage{Bytes: 10}},
			}
			// Policy and download fields may advance after the authorization read.
			currentTarget, currentSource := target, source
			currentTarget.DownloadCount, currentTarget.DownloadsRemaining = 7, 9
			currentTarget.PasswordHash, currentTarget.ExpireAt = "current-password", 2500
			var wantErr error
			switch scenario {
			case "shrink-over-limit":
				change.Limits = uploadquota.Limits{}
			case "source-owner-changed":
				currentSource.UserId = 3
				wantErr = uploadquota.ErrConflict
			case "target-owner-changed":
				currentTarget.UserId = 3
				wantErr = uploadquota.ErrConflict
			case "source-text-changed":
				currentSource.Name = "newer-source.bin"
				wantErr = uploadquota.ErrConflict
			case "delete-source":
				change.DeleteSource = true
			case "quota-reject-delete":
				change.DeleteSource, change.Limits.User.Bytes = true, 4
				wantErr = uploadquota.ErrQuota
			}
			p.SaveMetaData(currentTarget)
			p.SaveMetaData(currentSource)
			before, err := p.ReadUploadQuota(ctx)
			if err != nil {
				t.Fatal(err)
			}
			record, err := p.ApplyUploadQuota(ctx, change)
			if !errors.Is(err, wantErr) || !reflect.DeepEqual(record, uploadquota.Reservation{}) {
				t.Fatalf("replacement = %v; want %v with no reservation", err, wantErr)
			}
			if wantErr == nil {
				currentTarget.Name, currentTarget.Size, currentTarget.SHA1 = source.Name, source.Size, source.SHA1
				currentTarget.ContentType, currentTarget.AwsBucket = source.ContentType, source.AwsBucket
				currentTarget.SizeBytes, currentTarget.Encryption = source.SizeBytes, source.Encryption
				if change.DeleteSource {
					currentSource.ExpireAt, currentSource.UnlimitedTime = 0, false
				}
			}
			for _, expected := range []models.File{currentTarget, currentSource} {
				if got, ok := p.GetMetaDataById(expected.Id); !ok || !reflect.DeepEqual(got, expected) {
					t.Fatal("replacement changed unrelated metadata or partially committed a rejected change")
				}
				if got, ok := p.GetHotlink(expected.HotlinkId); !ok || got != expected.Id {
					t.Fatal("replacement changed an existing hotlink mapping")
				}
			}
			after, err := p.ReadUploadQuota(ctx)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("replacement changed the reservation ledger", err)
			}
		})
	}
}
