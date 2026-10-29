//go:build test && noaws && security_deletion

package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
)

const deletionStateOwner = 9401

// The storage TestMain owns global configuration and one database. These tests
// run serially and replace/restore both together, as the existing storage quota
// fixtures do. No existing fixture blob is used for a deletion assertion.
func deletionStateStore(t *testing.T) string {
	t.Helper()
	saved := *configuration.Get()
	previous, err := database.ParseUrl(saved.DatabaseUrl, true)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	connection := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "deletion-state.sqlite")}
	database.Close()
	database.Connect(connection)
	t.Cleanup(func() {
		database.DeleteUser(deletionStateOwner)
		database.DeleteUser(deletionStateOwner + 1)
		database.Close()
		*configuration.Get() = saved
		database.Connect(previous)
		filesystem.Init(saved.DataDir)
	})
	configuration.Get().DataDir = dir
	configuration.Get().DatabaseUrl = "sqlite://" + connection.HostUrl
	filesystem.Init(dir)
	database.SaveUser(models.User{Id: deletionStateOwner, Name: "Synthetic deletion state owner", UserLevel: models.UserLevelUser}, false)
	database.SaveUser(models.User{Id: deletionStateOwner + 1, Name: "Synthetic deletion state other owner", UserLevel: models.UserLevelUser}, false)
	return dir
}

func deletionStateFile(t *testing.T, dir, id string) models.File {
	t.Helper()
	file := models.File{Id: id, UserId: deletionStateOwner, Name: "synthetic.bin", Size: "3 B", SizeBytes: 3,
		SHA1: id + "-blob", HotlinkId: id + "-link", ContentType: "application/octet-stream", PasswordHash: "synthetic-old-hash",
		ExpireAt: time.Now().Add(time.Hour).Unix(), UnlimitedTime: true, DownloadCount: 3, DownloadsRemaining: 8}
	if err := os.WriteFile(filepath.Join(dir, file.SHA1), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	database.SaveMetaData(file)
	database.SaveHotlink(file)
	t.Cleanup(func() {
		deletionTimers.invalidate(file.Id)
		downloadstatus.SetAllComplete(file.Id)
		database.DeleteHotlink(file.HotlinkId)
		database.DeleteMetaData(file.Id)
	})
	return file
}

func TestSEC040DeletionStateIdentityAndLogicalExpire(t *testing.T) {
	for _, scenario := range []string{"owner-changed", "locator-changed", "logical-expire"} {
		t.Run(scenario, func(t *testing.T) {
			dir := deletionStateStore(t)
			file := deletionStateFile(t, dir, "state-identity")
			current := file
			switch scenario {
			case "owner-changed":
				current.UserId++
			case "locator-changed":
				current.SHA1 += "-new"
				if err := os.WriteFile(filepath.Join(dir, current.SHA1), []byte("abc"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			database.SaveMetaData(current)
			result, err := DeleteFileContext(context.Background(), file, false)
			if scenario == "logical-expire" {
				current.ExpireAt, current.UnlimitedTime = 0, false
				if err != nil || result.State != DeletionAccepted || !reflect.DeepEqual(result.File, current) {
					t.Fatal("logical deletion did not return exactly the expired metadata", err)
				}
			} else if !errors.Is(err, uploadquota.ErrConflict) || result.State != "" {
				t.Fatal("stale authorized identity was accepted", err)
			}
			stored, found := database.GetMetaDataById(file.Id)
			if !found || !reflect.DeepEqual(stored, current) {
				t.Fatal("deletion overwrote current owner, locator or unrelated fields")
			}
			if content, err := os.ReadFile(filepath.Join(dir, current.SHA1)); err != nil || string(content) != "abc" {
				t.Fatal("logical or rejected deletion touched the source", err)
			}
			if id, found := database.GetHotlink(file.HotlinkId); !found || id != file.Id {
				t.Fatal("logical or rejected deletion retired the hotlink")
			}
		})
	}
}

func TestSEC040DeletionStateCancelRequiresLiveSource(t *testing.T) {
	for _, scenario := range []string{"active", "missing-source", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			dir := deletionStateStore(t)
			file := deletionStateFile(t, dir, "state-cancel")
			plan, err := ScheduleFileDeletionContext(context.Background(), file, int(time.Hour/time.Millisecond), false)
			if err != nil || plan.File.PendingDeletion == 0 {
				t.Fatal("fixture could not schedule a live file", err)
			}
			current := plan.File
			switch scenario {
			case "missing-source":
				if err := os.Remove(filepath.Join(dir, file.SHA1)); err != nil {
					t.Fatal(err)
				}
			case "expired":
				current.ExpireAt, current.UnlimitedTime = 1, false
				database.SaveMetaData(current)
			}
			got, err := CancelFileDeletionContext(context.Background(), plan.File)
			if scenario == "active" {
				current.PendingDeletion = 0
				if err != nil || !reflect.DeepEqual(got, current) {
					t.Fatal("live cancellation did not preserve current metadata", err)
				}
			} else if !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatal("cancellation acknowledged an expired or missing source", err)
			}
			stored, found := database.GetMetaDataById(file.Id)
			if !found || !reflect.DeepEqual(stored, current) {
				t.Fatal("cancellation lost the pending anchor or unrelated metadata")
			}
		})
	}
}

func TestSEC040DeletionStateStaleTimerToken(t *testing.T) {
	dir := deletionStateStore(t)
	file := deletionStateFile(t, dir, "state-old-token")
	file.PendingDeletion = time.Now().Add(-time.Minute).Unix()
	database.SaveMetaData(file)
	oldToken, latestToken := &deletionTimerToken{}, &deletionTimerToken{}
	// Model a superseded callback without reproducing the private HTTP timing
	// scenario or arming another background timer.
	deletionTimers.mu.Lock()
	deletionTimers.entries[file.Id] = &deletionTimerEntry{id: file.Id, token: latestToken, hasSpec: true,
		spec: deletionTimerSpec{File: file, Deadline: time.Now().Add(-time.Second)}}
	deletionTimers.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, file.Id)
	if err != nil {
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(unlock) }
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		close(started)
		defer close(done)
		executeDeletionTimer(file.Id, oldToken)
	}()
	t.Cleanup(func() {
		release()
		select {
		case <-done:
		case <-time.After(6 * time.Second):
			t.Error("old timer callback did not stop before fixture cleanup")
		}
	})
	<-started
	release()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("old timer callback did not finish", ctx.Err())
	}
	stored, found := database.GetMetaDataById(file.Id)
	if !found || !reflect.DeepEqual(stored, file) {
		t.Fatal("a stale timer token changed the current pending intent")
	}
	if _, current := deletionTimers.current(file.Id, latestToken); !current {
		t.Fatal("stale callback invalidated the current timer token")
	}
}

func TestSEC040DeletionStatePasswordDoesNotResurrect(t *testing.T) {
	dir := deletionStateStore(t)
	file := deletionStateFile(t, dir, "state-password")
	if changed, err := UpgradeFilePassword(context.Background(), file.Id, "stale-hash", "synthetic-new-hash"); !errors.Is(err, uploadquota.ErrConflict) || changed {
		t.Fatal("password upgrade accepted an unverified old hash", err)
	}
	if stored, found := database.GetMetaDataById(file.Id); !found || !reflect.DeepEqual(stored, file) {
		t.Fatal("stale password upgrade changed metadata")
	}
	if changed, err := UpgradeFilePassword(context.Background(), file.Id, file.PasswordHash, "synthetic-new-hash"); err != nil || !changed {
		t.Fatal("verified password upgrade was rejected", err)
	}
	file.PasswordHash = "synthetic-new-hash"
	if stored, found := database.GetMetaDataById(file.Id); !found || !reflect.DeepEqual(stored, file) {
		t.Fatal("password upgrade overwrote policy, owner or counters")
	}
	database.DeleteMetaData(file.Id)
	if changed, err := UpgradeFilePassword(context.Background(), file.Id, file.PasswordHash, "another-synthetic-hash"); err != nil || changed {
		t.Fatal("missing password target was recreated", err)
	}
	if _, found := database.GetMetaDataById(file.Id); found {
		t.Fatal("password upgrade resurrected retired metadata")
	}
}
