//go:build test && noaws && security_deletion

package filerequest

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// This package's other tests are pure display projections. Keep the one real
// database/configuration fixture serial and restore any pre-existing settings.
func TestSEC040RequestDeletionRevokesClaimedCommitOnCleanupFailure(t *testing.T) {
	saved := *configuration.Get()
	var previous *models.DbConnection
	if saved.DatabaseUrl != "" {
		parsed, err := database.ParseUrl(saved.DatabaseUrl, true)
		if err != nil {
			t.Fatal("SETUP existing test database configuration", err)
		}
		previous = &parsed
		database.Close()
	}
	dir := t.TempDir()
	connection := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "request-deletion.sqlite")}
	database.Connect(connection)
	t.Cleanup(func() {
		database.Close()
		*configuration.Get() = saved
		if previous != nil {
			database.Connect(*previous)
		}
		if saved.DataDir != "" {
			filesystem.Init(saved.DataDir)
		}
	})
	configuration.Get().DataDir = dir
	configuration.Get().DatabaseUrl = "sqlite://" + connection.HostUrl
	filesystem.Init(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Now().Unix()
	owner := models.User{Id: 9511, Name: "Synthetic request deletion owner", UserLevel: models.UserLevelUser}
	otherOwner := models.User{Id: 9512, Name: "Synthetic retained request owner", UserLevel: models.UserLevelUser}
	database.SaveUser(owner, false)
	database.SaveUser(otherOwner, false)
	request := models.FileRequest{Id: "request-delete-contract", UserId: owner.Id, Name: "Synthetic request",
		ApiKey: "synthetic-request-delete-key", MaxFiles: 10, MaxSize: 1, Expiry: now + 3600}
	database.SaveFileRequest(request)
	database.SaveApiKey(models.ApiKey{Id: request.ApiKey, PublicId: "synthetic-request-delete-public",
		UserId: owner.Id, UploadRequestId: request.Id, Permissions: models.ApiPermUpload})
	if stored, found := database.GetFileRequest(request.Id); !found || stored.UserId != owner.Id || stored.Expiry <= now {
		t.Fatal("SETUP current owned request policy was not persisted")
	}
	if key, found := database.GetApiKey(request.ApiKey); !found || key.UserId != owner.Id || key.UploadRequestId != request.Id {
		t.Fatal("SETUP request upload key was not persisted")
	}
	ownedPayload, otherPayload, latePayload := []byte("owned"), []byte("other"), []byte("late")
	ownedSum, otherSum, lateSum := sha1.Sum(ownedPayload), sha1.Sum(otherPayload), sha1.Sum(latePayload) // Product blob locators, not password hashes.
	owned := models.File{Id: "request-owned-source", UserId: owner.Id, UploadRequestId: request.Id,
		SHA1: hex.EncodeToString(ownedSum[:]), Name: "synthetic-owned.bin", Size: "5 B", SizeBytes: int64(len(ownedPayload)),
		ContentType: "application/octet-stream", UnlimitedTime: true, UnlimitedDownloads: true, ExpireAt: now + 3600}
	other := owned
	other.Id, other.UserId, other.SHA1, other.Name = "request-other-source", otherOwner.Id, hex.EncodeToString(otherSum[:]), "synthetic-other.bin"
	late := owned
	late.Id, late.SHA1, late.Name, late.Size, late.SizeBytes = "request-late-file", hex.EncodeToString(lateSum[:]), "synthetic-late.bin", "4 B", int64(len(latePayload))
	for _, input := range []struct {
		file    models.File
		payload []byte
	}{{owned, ownedPayload}, {other, otherPayload}, {late, latePayload}} {
		if err := os.WriteFile(filepath.Join(dir, input.file.SHA1), input.payload, 0600); err != nil {
			t.Fatal("SETUP isolated source write", err)
		}
	}
	database.SaveMetaData(owned)
	database.SaveMetaData(other)
	if current, found, err := database.ReadRecoveryFile(ctx, owned.Id); err != nil || !found || !reflect.DeepEqual(current, owned) {
		t.Fatal("SETUP checked source metadata was not persisted", err)
	}
	limits := uploadquota.Limits{
		User: uploadquota.Usage{Bytes: 100, Files: 10}, Request: uploadquota.Usage{Bytes: 100, Files: 10},
		Instance: uploadquota.Usage{Bytes: 200, Files: 20}, UserActive: 4, RequestActive: 4, InstanceActive: 8, StagingBytes: 1024,
	}
	change := uploadquota.Change{Action: uploadquota.Reserve, Now: now, Limits: limits, CheckRequestPolicy: true,
		Upload: uploadquota.Reservation{ID: "request-claimed-upload", UserID: owner.Id, RequestID: request.Id,
			BytesKnown: true, Bytes: late.SizeBytes, StagingBytes: late.SizeBytes * 2, ExpiresAt: now + 1800}}
	if _, err := database.ApplyUploadQuota(ctx, change); err != nil {
		t.Fatal("SETUP normal request reservation was rejected", err)
	}
	change.Action, change.FileID = uploadquota.Claim, late.Id
	claimed, err := database.ApplyUploadQuota(ctx, change)
	if err != nil || !claimed.Completing || claimed.State != uploadquota.Uploading || claimed.FileID != late.Id || claimed.RequestID != request.Id {
		t.Fatal("SETUP normal request completion was not claimed", err)
	}
	if _, found, err := database.ReadRecoveryFile(ctx, late.Id); err != nil || found {
		t.Fatal("SETUP claimed upload already published metadata", err)
	}
	// Turn only this owned synthetic blob into a directory. Cleanup must refuse
	// it while preserving the revoked metadata as a management retry anchor.
	ownedPath := filepath.Join(dir, owned.SHA1)
	if err := os.Remove(ownedPath); err != nil {
		t.Fatal("SETUP synthetic fault preparation", err)
	}
	if err := os.Mkdir(ownedPath, 0700); err != nil {
		t.Fatal("SETUP synthetic directory fault", err)
	}
	deleteErr := DeleteContext(ctx, request)
	if !errors.Is(deleteErr, uploadquota.ErrInvalid) || errors.Is(deleteErr, context.Canceled) || errors.Is(deleteErr, context.DeadlineExceeded) {
		t.Fatal("SETUP expected checked cleanup refusal was not reached", deleteErr)
	}
	retained, found := database.GetFileRequest(request.Id)
	if !found || retained.Expiry == 0 || retained.Expiry >= time.Now().Unix() {
		t.Error("SEC040_REQUEST_DELETE failed cleanup left the retained request upload policy live")
	}
	if _, found := database.GetApiKey(request.ApiKey); found {
		t.Error("SEC040_REQUEST_DELETE upload key was not revoked")
	}
	wantOwned := owned
	wantOwned.ExpireAt, wantOwned.UnlimitedTime, wantOwned.PendingDeletion = 0, false, 0
	if current, found, err := database.ReadRecoveryFile(ctx, owned.Id); err != nil || !found || !reflect.DeepEqual(current, wantOwned) {
		t.Error("SEC040_REQUEST_DELETE failed cleanup lost or failed to revoke the original management anchor", err)
	}
	if info, err := os.Lstat(ownedPath); err != nil || !info.IsDir() {
		t.Error("SEC040_REQUEST_DELETE cleanup altered the refused directory", err)
	}
	if current, found, err := database.ReadRecoveryFile(ctx, other.Id); err != nil || !found || !reflect.DeepEqual(current, other) {
		t.Error("SEC040_REQUEST_DELETE modified another owner's metadata with the same request ID", err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, other.SHA1)); err != nil || string(content) != string(otherPayload) {
		t.Error("SEC040_REQUEST_DELETE altered another owner's source", err)
	}
	beforeCommit, err := database.ReadUploadQuota(ctx)
	if err != nil {
		t.Fatal("SETUP quota snapshot before the late commit", err)
	}
	change.Action, change.File, change.Now = uploadquota.Commit, &late, time.Now().Unix()
	if change.Now >= claimed.ExpiresAt {
		t.Fatal("SETUP reservation itself expired before the policy check")
	}
	_, commitErr := database.ApplyUploadQuota(ctx, change)
	if !errors.Is(commitErr, uploadquota.ErrExpired) {
		t.Error("SEC040_REQUEST_DELETE already-claimed upload was not rejected by the expired request policy", commitErr)
	}
	if _, found, err := database.ReadRecoveryFile(ctx, late.Id); err != nil || found {
		t.Error("SEC040_REQUEST_DELETE late commit published metadata after request deletion failed", err)
	}
	afterCommit, err := database.ReadUploadQuota(ctx)
	if err != nil || !reflect.DeepEqual(beforeCommit, afterCommit) {
		t.Error("SEC040_REQUEST_DELETE rejected late commit changed the reservation ledger", err)
	}
}
