//go:build test && noaws && security_reclaim

package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/blobreclaim"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"github.com/alicebob/miniredis/v2"
)

const reclaimTestOwner = 9411

// These tests follow the storage package's serial fixture contract, swapping
// configuration and its database together. Only new synthetic canonical bytes
// in this disposable directory may be removed.
func reclaimTestStore(t *testing.T, backend string) string {
	t.Helper()
	saved := *configuration.Get()
	previous, err := database.ParseUrl(saved.DatabaseUrl, true)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	connection := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "reclaim.sqlite")}
	if backend == "redis" {
		connection = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "reclaim_"}
	}
	database.Close()
	database.Connect(connection)
	t.Cleanup(func() {
		database.Close()
		*configuration.Get() = saved
		database.Connect(previous)
		filesystem.Init(saved.DataDir)
	})
	configuration.Get().DataDir = dir
	configuration.Get().DatabaseUrl = "sqlite://" + connection.HostUrl
	if backend == "redis" {
		configuration.Get().DatabaseUrl = "redis://" + connection.HostUrl + "?prefix=" + connection.RedisPrefix
	}
	configuration.Get().Encryption.Level = encryption.NoEncryption
	filesystem.Init(dir)
	for offset, name := range []string{"synthetic-reclaim-owner", "synthetic-reclaim-other"} {
		database.SaveUser(models.User{Id: reclaimTestOwner + offset, Name: name, UserLevel: models.UserLevelUser}, false)
	}
	return dir
}

func reclaimTestFile(t *testing.T, dir, id string, value byte) models.File {
	t.Helper()
	payload := bytes.Repeat([]byte{value}, 64)
	hash := sha1.Sum(payload) // Existing locator convention, not a new integrity protocol.
	file := models.File{Id: id, UserId: reclaimTestOwner, Name: id + ".bin", Size: "64 B", SizeBytes: 64,
		SHA1: hex.EncodeToString(hash[:]), ContentType: "application/octet-stream", UnlimitedTime: true, UnlimitedDownloads: true}
	if err := os.WriteFile(filepath.Join(dir, file.SHA1), payload, 0600); err != nil {
		t.Fatal(err)
	}
	database.SaveMetaData(file)
	return file
}

func reclaimTestReplace(t *testing.T, target, source models.File) models.File {
	t.Helper()
	unlock, err := apimutex.LockContext(t.Context(), apimutex.TypeMetaData, target.Id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	updated, err := ReplaceFileContext(t.Context(), target, source, false)
	if err != nil {
		t.Fatal("synthetic replacement failed", err)
	}
	return updated
}

func reclaimTestCandidate(t *testing.T, file models.File) blobreclaim.Candidate {
	t.Helper()
	candidates, err := database.ReadBlobCandidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.SHA1 == file.SHA1 && candidate.AwsBucket == file.AwsBucket {
			return candidate
		}
	}
	t.Fatal("old locator has no durable candidate")
	return blobreclaim.Candidate{}
}

func reclaimTestBytes(t *testing.T, dir string, file models.File, value byte) {
	t.Helper()
	if got, err := os.ReadFile(filepath.Join(dir, file.SHA1)); err != nil || !bytes.Equal(got, bytes.Repeat([]byte{value}, 64)) {
		t.Fatal("canonical bytes were changed or lost", err)
	}
}

func reclaimTestPublicationLimits(t *testing.T) uploadquota.Limits {
	t.Helper()
	limits, err := metadataLimits()
	if err != nil {
		t.Fatal(err)
	}
	// Metadata-only limits intentionally contain no upload slots. The controlled
	// Reserve/Claim fixture needs its own explicit finite active/stage capacities.
	limits.UserActive, limits.InstanceActive, limits.RequestActive = 2, 4, 2
	limits.StagingBytes = 1 << 20
	return limits
}

func TestSEC041ReclaimReferenceAndSameTarget(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			dir := reclaimTestStore(t, backend)
			target := reclaimTestFile(t, dir, "reclaim-target", 'A')
			source := reclaimTestFile(t, dir, "reclaim-source", 'B')
			other := target
			other.Id, other.UserId = "reclaim-other", reclaimTestOwner+1
			database.SaveMetaData(other)
			updated := reclaimTestReplace(t, target, source)
			old := reclaimTestCandidate(t, target)
			// A new source can point the original target ID back to A. Remove
			// only the synthetic extra row, leaving target as A's sole reference.
			updated = reclaimTestReplace(t, updated, other)
			database.DeleteMetaData(other.Id)
			before, err := database.ReadUploadQuota(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			result, err := reclaimBlobCandidateContext(t.Context(), old, dir)
			if err != nil || result.Shared != 1 || result.Deleted != 0 || result.Retired != 1 {
				t.Fatal("original target reference did not preserve old content", err)
			}
			reclaimTestBytes(t, dir, target, 'A')
			if stored, found := database.GetMetaDataById(target.Id); !found || !reflect.DeepEqual(stored, updated) {
				t.Fatal("reclamation changed the surviving target")
			}
			// Later replacement must create a fresh intent after shared ack.
			updated = reclaimTestReplace(t, updated, source)
			fresh := reclaimTestCandidate(t, target)
			if fresh.Token == old.Token {
				t.Fatal("later replacement reused the old intent")
			}
			if _, err := reclaimBlobCandidateContext(t.Context(), old, dir); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatal("stale scan was allowed to perform old-content cleanup", err)
			}
			reclaimTestBytes(t, dir, target, 'A')
			if result, err := reclaimBlobCandidateContext(t.Context(), fresh, dir); err != nil || result.Deleted != 1 {
				t.Fatal("unreferenced old content was not reclaimed", err)
			}
			if _, err := os.Lstat(filepath.Join(dir, target.SHA1)); !os.IsNotExist(err) {
				t.Fatal("old source absence not confirmed")
			}
			if result, err := reclaimBlobCandidateContext(t.Context(), fresh, dir); err != nil || result.Retired != 0 {
				t.Fatal("repeated candidate cleanup was not idempotent", err)
			}
			reclaimTestBytes(t, dir, source, 'B')
			if after, err := database.ReadUploadQuota(t.Context()); err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("candidate cleanup changed quota accounting", err)
			}
		})
	}
}

func TestSEC041ReclaimSharedPoliciesAndFailure(t *testing.T) {
	for _, scenario := range []string{"cross-owner", "expired", "pending", "file-request", "unsafe-source", "retired-scan"} {
		t.Run(scenario, func(t *testing.T) {
			dir := reclaimTestStore(t, "sqlite")
			target := reclaimTestFile(t, dir, "policy-target", 'A')
			source := reclaimTestFile(t, dir, "policy-source", 'B')
			reclaimTestReplace(t, target, source)
			candidate := reclaimTestCandidate(t, target)
			if scenario == "unsafe-source" || scenario == "retired-scan" {
				if err := os.Remove(filepath.Join(dir, target.SHA1)); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dir, target.SHA1), 0700); err != nil {
					t.Fatal(err)
				}
				if scenario == "retired-scan" {
					if retired, err := database.RetireBlobCandidate(t.Context(), candidate); err != nil || !retired {
						t.Fatal(err)
					}
					if result, err := reclaimBlobCandidateContext(t.Context(), candidate, dir); err != nil || result.Retired != 0 {
						t.Fatal("retired scan probed a new unowned object", err)
					}
					return
				}
				if _, err := reclaimBlobCandidateContext(t.Context(), candidate, dir); err == nil {
					t.Fatal("non-regular source was treated as deleted")
				}
				if current := reclaimTestCandidate(t, target); current != candidate {
					t.Fatal("failed cleanup lost or changed retry intent")
				}
				if err := os.Remove(filepath.Join(dir, target.SHA1)); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, target.SHA1), bytes.Repeat([]byte{'A'}, 64), 0600); err != nil {
					t.Fatal(err)
				}
				if result, err := reclaimBlobCandidateContext(t.Context(), candidate, dir); err != nil || result.Deleted != 1 {
					t.Fatal("restored source could not be retried", err)
				}
				return
			}
			other := target
			other.Id, other.UserId = "policy-other", reclaimTestOwner+1
			switch scenario {
			case "expired":
				other.UnlimitedTime, other.ExpireAt = false, 1
			case "pending":
				other.PendingDeletion = 1
			case "file-request":
				other.UploadRequestId = "synthetic-request"
			}
			database.SaveMetaData(other)
			if result, err := reclaimBlobCandidateContext(t.Context(), candidate, dir); err != nil || result.Shared != 1 {
				t.Fatal("referenced content was not retained", err)
			}
			reclaimTestBytes(t, dir, target, 'A')
			if stored, found := database.GetMetaDataById(other.Id); !found || !reflect.DeepEqual(stored, other) {
				t.Fatal("candidate cleanup changed another share's metadata")
			}
		})
	}
}

func TestSEC041ReclaimPublicationAndUnknownClaim(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			dir := reclaimTestStore(t, backend)
			target := reclaimTestFile(t, dir, "publication-target", 'A')
			source := reclaimTestFile(t, dir, "publication-source", 'B')
			reclaimTestReplace(t, target, source)
			candidate := reclaimTestCandidate(t, target)
			now := time.Now().Unix()
			limits := reclaimTestPublicationLimits(t)
			upload := uploadquota.Reservation{ID: "synthetic-inflight", UserID: reclaimTestOwner + 1,
				BytesKnown: true, Bytes: 64, StagingBytes: 64, ExpiresAt: now + 3600}
			if _, err := database.ApplyUploadQuota(t.Context(), uploadquota.Change{Action: uploadquota.Reserve, Upload: upload, Limits: limits, Now: now}); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ApplyUploadQuota(t.Context(), uploadquota.Change{Action: uploadquota.Claim, Upload: upload, FileID: "publication-new", Now: now}); err != nil {
				t.Fatal(err)
			}
			if _, err := reclaimBlobCandidateContext(t.Context(), candidate, dir); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("unmapped claim was treated as an unreferenced blob", err)
			}
			reclaimTestBytes(t, dir, target, 'A')
			if current := reclaimTestCandidate(t, target); current != candidate {
				t.Fatal("unknown publication lost its candidate")
			}
			// Exercise the actual publication primitive while owning its blob
			// gate. A candidate worker must wait rather than inspect/delete A.
			unlock, err := lockBlobPublication(t.Context(), target)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			_, blocked := reclaimBlobCandidateContext(ctx, candidate, dir)
			cancel()
			if !errors.Is(blocked, context.DeadlineExceeded) {
				unlock()
				t.Fatal("candidate worker did not wait for publication gate", blocked)
			}
			published := target
			published.Id, published.UserId = "publication-new", upload.UserID
			_, commitErr := database.ApplyUploadQuota(t.Context(), uploadquota.Change{Action: uploadquota.Commit, Upload: upload, File: &published, Limits: limits, Now: now})
			unlock()
			if commitErr != nil {
				t.Fatal("controlled publication did not commit", commitErr)
			}
			if _, err := reclaimBlobCandidateContext(t.Context(), candidate, dir); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("committed but unconfirmed stage was treated as safe to reclaim", err)
			}
			reclaimTestBytes(t, dir, published, 'A')
			// This fixture has no private input or worker I/O; its empty stage
			// is known cleaned. Release through the real ledger transition.
			_, releaseErr := database.ApplyUploadQuota(t.Context(), uploadquota.Change{Action: uploadquota.ReleaseStaging, Upload: upload, Now: now, CleanupConfirmed: true})
			if releaseErr != nil {
				t.Fatal("controlled publication did not settle", releaseErr)
			}
			if result, err := reclaimBlobCandidateContext(t.Context(), candidate, dir); err != nil || result.Shared != 1 {
				t.Fatal("committed new reference did not prevent deletion", err)
			}
			reclaimTestBytes(t, dir, published, 'A')
			if current, found := database.GetMetaDataById(published.Id); !found || !reflect.DeepEqual(current, published) {
				t.Fatal("new publication was changed")
			}
		})
	}
}

func TestSEC041ReclaimBoundedPassContinuesAfterFailure(t *testing.T) {
	dir := reclaimTestStore(t, "sqlite")
	// The fixture owns this native absolute path; avoid interpreting a Windows
	// drive letter as a URI host/port when opening its second SQLite connection.
	const sqlitePrefix = "sqlite://"
	if !strings.HasPrefix(configuration.Get().DatabaseUrl, sqlitePrefix) {
		t.Fatal("bounded pass fixture is not SQLite")
	}
	config := models.DbConnection{HostUrl: strings.TrimPrefix(configuration.Get().DatabaseUrl, sqlitePrefix)}
	provider, err := dbabstraction.GetNew(config)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	candidates := make([]blobreclaim.Candidate, oldContentBatchSize+1)
	for i := range candidates {
		candidates[i], err = blobreclaim.New(fmt.Sprintf("synthetic-batch-%03d", i), "")
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key() < candidates[j].Key() })
	if err := provider.ImportBlobCandidates(t.Context(), candidates); err != nil {
		t.Fatal(err)
	}
	failed := candidates[0]
	if err := os.Mkdir(filepath.Join(dir, failed.SHA1), 0700); err != nil {
		t.Fatal(err)
	}
	oldContentCursor.Lock()
	previous := oldContentCursor.key
	oldContentCursor.key = ""
	oldContentCursor.Unlock()
	t.Cleanup(func() {
		oldContentCursor.Lock()
		oldContentCursor.key = previous
		oldContentCursor.Unlock()
	})
	first, firstErr := reclaimOldContentContext(t.Context(), dir)
	if firstErr == nil || first.Retired != oldContentBatchSize-1 || first.Missing != oldContentBatchSize-1 {
		t.Fatal("bounded pass did not continue after its first failed source", first, firstErr)
	}
	if remaining, err := database.ReadBlobCandidates(t.Context()); err != nil || len(remaining) != 2 {
		t.Fatal("bounded pass dropped or over-processed its durable remainder", err)
	}
	second, secondErr := reclaimOldContentContext(t.Context(), dir)
	if secondErr == nil || second.Retired != 1 || second.Missing != 1 {
		t.Fatal("failed first source starved the next batch", second, secondErr)
	}
	if remaining, err := database.ReadBlobCandidates(t.Context()); err != nil || len(remaining) != 1 || remaining[0] != failed {
		t.Fatal("retry anchor did not survive both passes", err)
	}
}

// Candidate and ordinary metadata cleanup share one pass. An expired share of
// A cannot bypass the unknown-publication guard which preserved A's candidate.
func TestSEC041ReclaimUnknownClaimFullPass(t *testing.T) {
	dir := reclaimTestStore(t, "sqlite")
	target := reclaimTestFile(t, dir, "unknown-target", 'A')
	source := reclaimTestFile(t, dir, "unknown-source", 'B')
	reclaimTestReplace(t, target, source)
	candidate := reclaimTestCandidate(t, target)
	expired := target
	expired.Id, expired.UnlimitedTime, expired.ExpireAt = "unknown-expired-anchor", false, 1
	database.SaveMetaData(expired)
	limits := reclaimTestPublicationLimits(t)
	now := time.Now().Unix()
	upload := uploadquota.Reservation{ID: "unknown-publication", UserID: reclaimTestOwner,
		BytesKnown: true, Bytes: 64, StagingBytes: 64, ExpiresAt: now + 3600}
	if _, err := database.ApplyUploadQuota(t.Context(), uploadquota.Change{Action: uploadquota.Reserve, Upload: upload, Limits: limits, Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ApplyUploadQuota(t.Context(), uploadquota.Change{Action: uploadquota.Claim, Upload: upload, FileID: "unknown-result", Now: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := CleanUpContext(t.Context()); !errors.Is(err, uploadquota.ErrInProgress) {
		t.Fatal("unknown publication was not reported as pending", err)
	}
	reclaimTestBytes(t, dir, target, 'A')
	reclaimTestBytes(t, dir, source, 'B')
	if current := reclaimTestCandidate(t, target); current != candidate {
		t.Fatal("whole pass lost the old-content intent")
	}
	if current, found := database.GetMetaDataById(expired.Id); !found || !reflect.DeepEqual(current, expired) {
		t.Fatal("whole pass retired the last retry anchor of an unknown publication")
	}
}
