package storage

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
)

// DeletionResult describes this request, without exposing another share/owner.
type DeletionResult struct {
	File  models.File `json:"-"`
	State string      `json:"state"`
}

const (
	DeletionAccepted = "accepted"
	DeletionPending  = "pending"
	DeletionDeleted  = "deleted"
	DeletionShared   = "shared"
)

func sameDeletionIdentity(a, b models.File) bool {
	return a.Id == b.Id && a.UserId == b.UserId && a.SHA1 == b.SHA1 && a.AwsBucket == b.AwsBucket && a.UploadRequestId == b.UploadRequestId
}

func deletionContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, uploadquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupItemTimeout)
	return ctx, cancel, nil
}

func readDeletionFile(ctx context.Context, expected models.File) (models.File, error) {
	current, found, err := database.ReadRecoveryFile(ctx, expected.Id)
	if err != nil {
		return models.File{}, err
	}
	if !found {
		return models.File{}, ErrorFileNotFound
	}
	if !sameDeletionIdentity(expected, current) {
		return models.File{}, uploadquota.ErrConflict
	}
	return current, nil
}

func expireDeletionFileLocked(ctx context.Context, file models.File) (models.File, error) {
	updated, found, err := database.ApplyFileState(ctx, models.FileStatePatch{Expected: file, Operation: models.FileStateExpire})
	if err != nil {
		return models.File{}, err
	}
	if !found {
		return models.File{}, ErrorFileNotFound
	}
	deletionTimers.invalidate(file.Id)
	downloadstatus.SetAllComplete(file.Id)
	return updated, nil
}

// DeleteFileContext rechecks the authenticated identity under the metadata gate.
// Accepted state survives physical failure; callers must report the pending error.
func DeleteFileContext(ctx context.Context, expected models.File, physical bool) (DeletionResult, error) {
	var result DeletionResult
	ctx, cancel, err := deletionContext(ctx)
	if err != nil {
		return result, cleanupError{err}
	}
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, expected.Id)
	if err != nil {
		return result, cleanupError{err}
	}
	defer unlock()
	current, err := readDeletionFile(ctx, expected)
	if err != nil {
		return result, cleanupError{err}
	}
	current, err = expireDeletionFileLocked(ctx, current)
	if err != nil {
		return result, cleanupError{err}
	}
	result = DeletionResult{File: current, State: DeletionAccepted}
	if !physical {
		return result, nil
	}
	result.State = DeletionPending
	cleaned, err := cleanUpBlobReferenceLockedContext(ctx, current, time.Now().Unix(), configuration.Get().DataDir)
	if err != nil {
		return result, cleanupError{err}
	}
	if cleaned.Retired == 0 {
		return result, cleanupError{uploadquota.ErrConflict}
	}
	result.State = DeletionDeleted
	if cleaned.Shared != 0 {
		result.State = DeletionShared
	}
	return result, nil
}

// ScheduleFileDeletionContext preserves the public Unix-second metadata format.
// The live token/deadline disambiguates same-second plans within this process.
func ScheduleFileDeletionContext(ctx context.Context, expected models.File, delayMs int, physical bool) (DeletionResult, error) {
	var result DeletionResult
	if delayMs < 0 || int64(delayMs) > int64(1<<63-1)/int64(time.Millisecond) {
		return result, cleanupError{uploadquota.ErrInvalid}
	}
	ctx, cancel, err := deletionContext(ctx)
	if err != nil {
		return result, cleanupError{err}
	}
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, expected.Id)
	if err != nil {
		return result, cleanupError{err}
	}
	defer unlock()
	current, err := readDeletionFile(ctx, expected)
	if err != nil {
		return result, cleanupError{err}
	}
	if IsExpiredFile(current, time.Now().Unix()) {
		return result, cleanupError{uploadquota.ErrConflict}
	}
	reservation, err := deletionTimers.reserve(current.Id)
	if err != nil {
		return result, cleanupError{err}
	}
	defer reservation.abort()
	deadline := time.Now().Add(time.Duration(delayMs) * time.Millisecond)
	updated, found, err := database.ApplyFileState(ctx, models.FileStatePatch{Expected: current, Operation: models.FileStateSchedule, PendingDeletion: deadline.Unix()})
	if err != nil {
		// A lost COMMIT/EXEC reply may already have changed the persisted plan.
		// Suppress its old live callback; checked periodic GC retains the intent.
		deletionTimers.invalidate(current.Id)
		return result, cleanupError{err}
	}
	if !found {
		return result, cleanupError{ErrorFileNotFound}
	}
	reservation.commit(deletionTimerSpec{File: updated, Deadline: deadline, DeleteSource: physical})
	return DeletionResult{File: updated, State: DeletionAccepted}, nil
}

// CancelFileDeletionContext only acknowledges a live share with a verified source.
// Holding metadata before blob makes successful cancellation exclude stale GC.
func CancelFileDeletionContext(ctx context.Context, expected models.File) (models.File, error) {
	ctx, cancel, err := deletionContext(ctx)
	if err != nil {
		return models.File{}, cleanupError{err}
	}
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, expected.Id)
	if err != nil {
		return models.File{}, cleanupError{err}
	}
	defer unlock()
	current, err := readDeletionFile(ctx, expected)
	if err != nil {
		return models.File{}, cleanupError{err}
	}
	if IsExpiredFile(current, time.Now().Unix()) {
		return models.File{}, cleanupError{uploadquota.ErrConflict}
	}
	blobUnlock, err := lockBlobPublication(ctx, current)
	if err != nil {
		return models.File{}, cleanupError{err}
	}
	defer blobUnlock()
	var local *os.Root
	if current.IsLocalStorage() {
		if !cleanupLocalName(current.SHA1) {
			return models.File{}, cleanupError{uploadquota.ErrInvalid}
		}
		local, err = os.OpenRoot(configuration.Get().DataDir)
		if err != nil {
			return models.File{}, cleanupError{err}
		}
		defer local.Close()
	}
	exists, err := probeCleanupSource(ctx, current, local)
	if err != nil {
		return models.File{}, cleanupError{err}
	}
	if !exists {
		return models.File{}, cleanupError{uploadquota.ErrConflict}
	}
	updated, found, err := database.ApplyFileState(ctx, models.FileStatePatch{Expected: current, Operation: models.FileStateCancel})
	if err != nil {
		return models.File{}, cleanupError{err}
	}
	if !found {
		return models.File{}, cleanupError{ErrorFileNotFound}
	}
	deletionTimers.invalidate(current.Id)
	return updated, nil
}

func executeDeletionTimer(id string, token *deletionTimerToken) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupItemTimeout)
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, id)
	if err != nil {
		log.Print("Scheduled file deletion pending")
		return
	}
	defer unlock()
	spec, currentToken := deletionTimers.current(id, token)
	if !currentToken || time.Now().Before(spec.Deadline) {
		return
	}
	file, err := readDeletionFile(ctx, spec.File)
	if err != nil || file.PendingDeletion != spec.File.PendingDeletion {
		return
	}
	file, err = expireDeletionFileLocked(ctx, file)
	if err != nil {
		log.Print("Scheduled file deletion pending")
		return
	}
	if spec.DeleteSource {
		if _, err := cleanUpBlobReferenceLockedContext(ctx, file, time.Now().Unix(), configuration.Get().DataDir); err != nil {
			log.Print("Scheduled file cleanup pending")
		}
	}
}

// UpgradeFilePassword only replaces the hash it verified; it cannot recreate a
// retired row or overwrite a newer deletion/ownership snapshot.
func UpgradeFilePassword(ctx context.Context, id, expectedHash, newHash string) (bool, error) {
	ctx, cancel, err := deletionContext(ctx)
	if err != nil {
		return false, cleanupError{err}
	}
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, id)
	if err != nil {
		return false, cleanupError{err}
	}
	defer unlock()
	file, found, err := database.ReadRecoveryFile(ctx, id)
	if err != nil || !found {
		return false, err
	}
	if file.PasswordHash != expectedHash || file.IsPendingForDeletion() || IsExpiredFile(file, time.Now().Unix()) {
		return false, uploadquota.ErrConflict
	}
	_, found, err = database.ApplyFileState(ctx, models.FileStatePatch{Expected: file, Operation: models.FileStatePassword, PasswordHash: newHash})
	return found, err
}

func deletionFileSnapshot(id string) (models.File, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupItemTimeout)
	defer cancel()
	file, found, err := database.ReadRecoveryFile(ctx, id)
	return file, found && err == nil
}

func logDeletionFailure(err error) {
	if err != nil && !errors.Is(err, ErrorFileNotFound) {
		log.Print("File deletion pending")
	}
}

func cleanupAcceptedDeletion(file models.File) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupItemTimeout)
	defer cancel()
	_, err := cleanUpBlobReferenceContext(ctx, file, time.Now().Unix(), configuration.Get().DataDir)
	logDeletionFailure(err)
}

// DeleteFilesContext reports partial failure and retains every failed anchor.
// It never changes a row whose owner/locator no longer matches the input.
func DeleteFilesContext(ctx context.Context, files []models.File, physical bool) error {
	if ctx == nil {
		return cleanupError{uploadquota.ErrInvalid}
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupBatchTimeout)
	defer cancel()
	var firstErr error
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return cleanupError{err}
		}
		_, err := DeleteFileContext(ctx, file, physical)
		if err != nil && !errors.Is(err, ErrorFileNotFound) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ReadDeletionFilesContext returns a complete checked snapshot for a bounded
// management batch. An unreadable row cannot become an apparently empty batch.
func ReadDeletionFilesContext(ctx context.Context, owner int, requestID string) ([]models.File, error) {
	if ctx == nil || owner <= 0 {
		return nil, cleanupError{uploadquota.ErrInvalid}
	}
	ctx, cancel := context.WithTimeout(ctx, cleanupBatchTimeout)
	defer cancel()
	ids, err := database.ReadCleanupIDs(ctx)
	if err != nil {
		return nil, cleanupError{err}
	}
	var files []models.File
	for _, id := range ids {
		file, found, err := database.ReadRecoveryFile(ctx, id)
		if err != nil {
			return nil, cleanupError{err}
		}
		if found && file.UserId == owner && (requestID == "" || file.UploadRequestId == requestID) {
			files = append(files, file)
		}
	}
	return files, nil
}
