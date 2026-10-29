//go:build test && security_deletion

package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func sqliteStateFile() models.File {
	return models.File{Id: "state-file", UserId: 1, Name: "synthetic.bin", Size: "3 B", SizeBytes: 3,
		SHA1: "state-blob", HotlinkId: "state-link", ContentType: "application/octet-stream", UploadRequestId: "state-request",
		ExpireAt: 2000, UnlimitedTime: true, PendingDeletion: 1500, DownloadCount: 7, DownloadsRemaining: 8,
		PasswordHash: "synthetic-old-hash", Encryption: models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1, 2}}}
}

func TestSEC040StateSQLiteFieldPatch(t *testing.T) {
	for _, operation := range []models.FileStateOperation{models.FileStateExpire, models.FileStateSchedule, models.FileStateCancel, models.FileStatePassword} {
		t.Run(map[models.FileStateOperation]string{models.FileStateExpire: "expire", models.FileStateSchedule: "schedule", models.FileStateCancel: "cancel", models.FileStatePassword: "password"}[operation], func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			file := sqliteStateFile()
			if operation == models.FileStateExpire {
				// State revocation must not authorize physical access to this name.
				file.SHA1 = "../untrusted"
			}
			p.SaveMetaData(file)
			p.SaveHotlink(file)
			before, err := p.ReadUploadQuota(ctx)
			if err != nil {
				t.Fatal(err)
			}
			patch := models.FileStatePatch{Expected: file, Operation: operation}
			want := file
			switch operation {
			case models.FileStateExpire:
				want.ExpireAt, want.UnlimitedTime, want.PendingDeletion = 0, false, 0
			case models.FileStateSchedule:
				patch.PendingDeletion, want.PendingDeletion = 3000, 3000
			case models.FileStateCancel:
				want.PendingDeletion = 0
			case models.FileStatePassword:
				patch.PasswordHash, want.PasswordHash = "synthetic-new-hash", "synthetic-new-hash"
			}
			got, found, err := p.ApplyFileState(ctx, patch)
			if err != nil || !found || !reflect.DeepEqual(got, want) {
				t.Fatal("state patch did not return exactly the selected field changes", err)
			}
			stored, exists := p.GetMetaDataById(file.Id)
			if !exists || !reflect.DeepEqual(stored, want) {
				t.Fatal("state patch overwrote unrelated metadata")
			}
			after, err := p.ReadUploadQuota(ctx)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("state patch changed the reservation ledger", err)
			}
			if id, exists := p.GetHotlink(file.HotlinkId); !exists || id != file.Id {
				t.Fatal("state patch changed the hotlink")
			}
		})
	}
}

func TestSEC040StateSQLiteRejectsStaleAndFailedReads(t *testing.T) {
	for _, scenario := range []string{"counter", "owner", "missing", "cancelled", "corrupt", "invalid-schedule"} {
		t.Run(scenario, func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			file := sqliteStateFile()
			p.SaveMetaData(file)
			p.SaveHotlink(file)
			patch := models.FileStatePatch{Expected: file, Operation: models.FileStateCancel}
			current := file
			wantErr := uploadquota.ErrConflict
			switch scenario {
			case "counter":
				current.DownloadCount++
				p.SaveMetaData(current)
			case "owner":
				current.UserId++
				p.SaveMetaData(current)
			case "missing":
				p.DeleteMetaData(file.Id)
				wantErr = nil
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, wantErr = cancelled, context.Canceled
			case "corrupt":
				if _, err := p.sqliteDb.Exec("UPDATE FileMetaData SET Encryption = ? WHERE Id = ?", []byte("invalid-gob"), file.Id); err != nil {
					t.Fatal(err)
				}
				wantErr = uploadquota.ErrInvalid
			case "invalid-schedule":
				patch.Operation, wantErr = models.FileStateSchedule, uploadquota.ErrInvalid
			}
			got, found, err := p.ApplyFileState(ctx, patch)
			if !errors.Is(err, wantErr) || found || !reflect.DeepEqual(got, models.File{}) {
				t.Fatal("stale, absent or failed state patch was accepted", err)
			}
			if scenario == "corrupt" {
				var encoded []byte
				if err := p.sqliteDb.QueryRow("SELECT Encryption FROM FileMetaData WHERE Id = ?", file.Id).Scan(&encoded); err != nil || string(encoded) != "invalid-gob" {
					t.Fatal("corrupt metadata was changed", err)
				}
			} else {
				stored, exists := p.GetMetaDataById(file.Id)
				if scenario == "missing" {
					if exists {
						t.Fatal("state patch recreated missing metadata")
					}
				} else if !exists || !reflect.DeepEqual(stored, current) {
					t.Fatal("rejected state patch changed metadata")
				}
			}
			if id, exists := p.GetHotlink(file.HotlinkId); !exists || id != file.Id {
				t.Fatal("rejected state patch changed the hotlink")
			}
		})
	}
}
