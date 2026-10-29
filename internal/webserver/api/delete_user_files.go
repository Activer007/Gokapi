package api

import (
	"context"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
)

// Keep the user until every file transition succeeds. Earlier files may already
// have changed; a retry reads their current state rather than replaying old rows.
func deleteUserFiles(ctx context.Context, owner, target int, deleteFiles bool) error {
	files, err := storage.ReadDeletionFilesContext(ctx, owner, "")
	if err != nil {
		return err
	}
	if deleteFiles {
		return storage.DeleteFilesContext(ctx, files, true)
	}
	for _, file := range files {
		if err := transferDeletedUserFile(ctx, file, target); err != nil {
			return err
		}
	}
	return nil
}

func transferDeletedUserFile(ctx context.Context, expected models.File, target int) error {
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeMetaData, expected.Id)
	if err != nil {
		return err
	}
	defer unlock()
	current, found, err := database.ReadRecoveryFile(ctx, expected.Id)
	if err != nil || !found {
		return err
	}
	if current.UserId != expected.UserId || current.SHA1 != expected.SHA1 || current.AwsBucket != expected.AwsBucket || current.UploadRequestId != expected.UploadRequestId {
		return uploadquota.ErrConflict
	}
	_, err = storage.TransferFileOwnerContext(ctx, current, target)
	return err
}
