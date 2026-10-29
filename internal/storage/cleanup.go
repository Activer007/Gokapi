package storage

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem/s3filesystem/aws"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
)

const cleanupBatchTimeout = 30 * time.Second
const cleanupItemTimeout = 5 * time.Second

// CleanupResult separates confirmed share retirements from old-content intents.
// The top-level counters refer to metadata only. Deleted means the
// source was removed and verified absent; Shared means its blob was retained;
// Missing means the checked initial probe found no source. No IDs are exposed.
type CleanupResult struct {
	Retired    int
	Deleted    int
	Shared     int
	Missing    int
	OldContent OldContentCleanupResult
}

// Keep diagnostics opaque while preserving errors.Is for cancellation and
// provider conflicts. Callers must not log an underlying path or object key.
type cleanupError struct{ cause error }

func (err cleanupError) Error() string { return "file cleanup pending" }
func (err cleanupError) Unwrap() error { return err.cause }

var errCleanupSourcePresent = errors.New("cleanup source absence unconfirmed")

// CleanUpContext runs canonical share and old-content passes. Session sweep and
// legacy auxiliary maintenance remain outside publication locks in CleanUp.
// A failed or incomplete read never authorizes physical deletion or retirement.
func CleanUpContext(ctx context.Context) (CleanupResult, error) {
	var result CleanupResult
	if ctx == nil {
		return result, cleanupError{uploadquota.ErrInvalid}
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupBatchTimeout)
	defer cancel()
	ids, err := database.ReadCleanupIDs(ctx)
	if err != nil {
		return result, cleanupError{err}
	}
	dataDir := configuration.Get().DataDir
	// Give old-content candidates a bounded turn before the metadata loop so
	// repeated slow shares cannot starve replacement recovery indefinitely.
	oldContent, firstErr := reclaimOldContentContext(ctx, dataDir)
	result.OldContent = oldContent
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return result, cleanupError{err}
		}
		itemCtx, cancelItem := context.WithTimeout(ctx, cleanupItemTimeout)
		file, exists, err := database.ReadRecoveryFile(itemCtx, id)
		var retired CleanupResult
		if err == nil && exists {
			retired, err = cleanUpBlobReferenceContext(itemCtx, file, time.Now().Unix(), dataDir)
		}
		cancelItem()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result.Retired += retired.Retired
		result.Deleted += retired.Deleted
		result.Shared += retired.Shared
		result.Missing += retired.Missing
	}
	if err := ctx.Err(); err != nil {
		return result, cleanupError{err}
	}
	if firstErr != nil {
		return result, cleanupError{firstErr}
	}
	return result, nil
}

// Serialize restored state before taking the publication lock. Never acquire
// metadata after blob: replacement already uses metadata -> blob -> database.
func cleanUpBlobReferenceContext(ctx context.Context, scanned models.File, now int64, dataDir string) (CleanupResult, error) {
	var result CleanupResult
	if ctx == nil || now <= 0 {
		return result, uploadquota.ErrInvalid
	}
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, scanned.Id)
	if err != nil {
		return result, err
	}
	defer unlock()
	return cleanUpBlobReferenceLockedContext(ctx, scanned, now, dataDir)
}

// The caller holds this file's metadata stripe throughout physical I/O.
func cleanUpBlobReferenceLockedContext(ctx context.Context, scanned models.File, now int64, dataDir string) (CleanupResult, error) {
	var result CleanupResult
	unlock, err := lockBlobPublication(ctx, scanned)
	if err != nil {
		return result, err
	}
	defer unlock()
	current, exists, err := database.ReadRecoveryFile(ctx, scanned.Id)
	if err != nil || !exists {
		return result, err
	}
	if current.SHA1 != scanned.SHA1 || current.AwsBucket != scanned.AwsBucket {
		return result, uploadquota.ErrConflict
	}
	var local *os.Root
	if current.IsLocalStorage() {
		if !cleanupLocalName(current.SHA1) || dataDir == "" {
			return result, uploadquota.ErrInvalid
		}
		// The explicitly configured root may be a trusted symlink. OpenRoot
		// anchors operations inside that directory even if its name changes.
		local, err = os.OpenRoot(dataDir)
		if err != nil {
			return result, err
		}
		defer local.Close()
	}
	sourceExists, err := probeCleanupSource(ctx, current, local)
	if err != nil {
		return result, err
	}
	if sourceExists && !isExpiredFileWithoutDownload(current, now) && !isPendingToBeDeleted(current, now) {
		return result, nil
	}
	shared, err := database.HasBlobReference(ctx, current)
	if err != nil {
		return result, err
	}
	sourceGone := !sourceExists
	deleted := false
	if sourceExists && !shared {
		if err := checkCanonicalDeletionPublication(ctx); err != nil {
			return result, err
		}
		if err := deleteSource(ctx, current, local); err != nil {
			return result, err
		}
		sourceExists, err = probeCleanupSource(ctx, current, local)
		if err != nil {
			return result, err
		}
		if sourceExists {
			return result, errCleanupSourcePresent
		}
		sourceGone, deleted = true, true
	}
	retired, err := database.RetireCleanupFile(ctx, current, now, sourceGone)
	if err != nil || !retired {
		return result, err
	}
	deletionTimers.invalidate(current.Id)
	result.Retired = 1
	switch {
	case deleted:
		result.Deleted = 1
	case sourceGone:
		result.Missing = 1
	default:
		result.Shared = 1
	}
	return result, nil
}

func cleanupLocalName(name string) bool {
	return helper.IsSafeLocalBlobName(name)
}

func probeCleanupSource(ctx context.Context, file models.File, local *os.Root) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !file.IsLocalStorage() {
		exists, _, err := aws.FileExistsContext(ctx, file)
		return exists, err
	}
	if local == nil || !cleanupLocalName(file.SHA1) {
		return false, uploadquota.ErrInvalid
	}
	info, err := local.Lstat(file.SHA1)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, uploadquota.ErrInvalid
	}
	return true, nil
}

// deleteSource reports errors and never retires metadata. The caller holds the
// locator lock and must confirm absence with a checked probe after this returns.
func deleteSource(ctx context.Context, file models.File, local *os.Root) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !file.IsLocalStorage() {
		deleted, err := aws.DeleteObjectContext(ctx, file)
		if err != nil {
			return err
		}
		if !deleted {
			return errCleanupSourcePresent
		}
		return nil
	}
	if local == nil || !cleanupLocalName(file.SHA1) {
		return uploadquota.ErrInvalid
	}
	exists, err := probeCleanupSource(ctx, file, local)
	if err != nil || !exists {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err = local.Remove(file.SHA1)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
