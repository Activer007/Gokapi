package storage

import (
	"context"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
	"github.com/forceu/gokapi/internal/webserver/downloadstatus"
)

// ReplaceFileContext accepts both authorized snapshots. Only content fields
// change; optional source expiration is committed with the replacement.
func ReplaceFileContext(ctx context.Context, target, source models.File, deleteSource bool) (models.File, error) {
	if ctx == nil {
		return models.File{}, uploadquota.ErrInvalid
	}
	if target.IsFileRequest() {
		return models.File{}, ErrorReplaceRequestFile
	}
	if target.Encryption.IsEndToEndEncrypted || source.Encryption.IsEndToEndEncrypted {
		return models.File{}, ErrorReplaceE2EFile
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	limits, err := metadataLimits()
	if err != nil {
		return models.File{}, err
	}
	change := uploadquota.Change{Action: uploadquota.ReplaceMetadata, Target: &target, Source: &source, DeleteSource: deleteSource,
		Upload: uploadquota.Reservation{ID: target.Id, UserID: target.UserId}, Limits: limits, Now: time.Now().Unix()}
	if err = uploadquota.ValidateReplacement(change); err != nil {
		return models.File{}, err
	}
	unlock, err := lockBlobPublication(ctx, source)
	if err != nil {
		return models.File{}, err
	}
	defer unlock()
	exists, _, err := probeWholeBlob(ctx, source)
	if err != nil {
		return models.File{}, err
	}
	if !exists {
		return models.File{}, ErrorFileNotFound
	}
	// Lock acquisition and the probe can wait. Match the duplication path's
	// admission-time expiry check rather than reusing the pre-wait timestamp.
	change.Now = time.Now().Unix()
	if _, err = database.ApplyUploadQuota(ctx, change); err != nil {
		return models.File{}, err
	}
	target.Name, target.Size, target.SHA1 = source.Name, source.Size, source.SHA1
	target.ContentType, target.AwsBucket, target.SizeBytes = source.ContentType, source.AwsBucket, source.SizeBytes
	target.Encryption = source.Encryption
	if deleteSource {
		downloadstatus.SetAllComplete(source.Id)
		go CleanUp(false)
	}
	return target, nil
}
