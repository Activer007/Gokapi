package filerequest

import (
	"context"
	"log"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
)

// New creates a new file request object. It is not stored yet,
// and an API key has to be generated manually
func New(user models.User) models.FileRequest {
	now := time.Now()
	return models.FileRequest{
		Id:               helper.GenerateRandomString(configuration.GetEnvironment().LengthId),
		UserId:           user.Id,
		CreationDate:     now.Unix(),
		Expiry:           now.Add(5 * 24 * time.Hour).Unix(),
		AllowedDownloads: 1,
		Name:             "Unnamed file request",
	}
}

// Get returns a file request object by its ID and populates it
func Get(id string) (models.FileRequest, bool) {
	result, ok := database.GetFileRequest(id)
	if !ok {
		return models.FileRequest{}, false
	}
	result.Populate(database.GetAllMetadata(), configuration.Get().MaxFileSizeMB)
	requests := []models.FileRequest{result}
	populateReservationDisplay(requests, database.ReadUploadQuota)
	return requests[0], true
}

// GetAll returns a list of all file requests and populates them
func GetAll() []models.FileRequest {
	result := database.GetAllFileRequests()
	if len(result) == 0 {
		return result
	}
	allFiles := database.GetAllMetadata()
	maxServerSize := configuration.Get().MaxFileSizeMB
	for i, request := range result {
		request.Populate(allFiles, maxServerSize)
		result[i] = request
	}
	populateReservationDisplay(result, database.ReadUploadQuota)
	return result
}

// This is a display projection, never admission or authorization. A single
// bounded ledger read serves the entire result. Legacy reservations remain
// visible during transition without summing a possibly duplicated upload.
func populateReservationDisplay(requests []models.FileRequest, read func(context.Context) (uploadquota.Ledger, error)) {
	if len(requests) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ledger, err := read(ctx)
	if err != nil || ledger.Version != uploadquota.LedgerVersion || ledger.Records == nil || len(ledger.Records) > uploadquota.MaxRecords {
		// Keep Populate's existing legacy count if the durable display is
		// unavailable. It may under-report durable reservations; the provider's
		// atomic policy, not this fallback, decides whether another upload fits.
		return
	}
	counts := make(map[string]int)
	for _, record := range ledger.Records {
		if record.RequestID != "" && (record.State == uploadquota.Reserved || record.State == uploadquota.Uploading) {
			// Expired active records still hold capacity until trusted cleanup.
			// Committed metadata is already in UploadedFiles, even if staging
			// cleanup is pending, so it must not be counted as a second file.
			counts[record.RequestID]++
		}
	}
	for i := range requests {
		requests[i].ReservedUploads = max(requests[i].ReservedUploads, counts[requests[i].Id])
	}
}

// Delete all files associated with a file request and the request itself
func Delete(request models.FileRequest) {
	if err := DeleteContext(context.Background(), request); err != nil {
		log.Print("File request deletion pending")
	}
}

// DeleteContext keeps the request available for a management retry until its
// files have been safely retired. The upload key is revoked before cleanup.
func DeleteContext(ctx context.Context, request models.FileRequest) error {
	if ctx == nil {
		return uploadquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	unlock, err := apimutex.LockContext(ctx, apimutex.TypeFileRequest, request.Id)
	if err != nil {
		return err
	}
	defer unlock()
	current, found := database.GetFileRequest(request.Id)
	if !found {
		return storage.ErrorFileNotFound
	}
	if current.UserId != request.UserId || current.ApiKey != request.ApiKey {
		return uploadquota.ErrConflict
	}
	// Commit checks the request policy in its own provider transaction. Freeze
	// that policy before enumeration: earlier commits are in the snapshot and
	// later ones are refused even if their HTTP request was already authorized.
	current.Expiry = 1
	database.SaveFileRequest(current)
	database.DeleteApiKey(current.ApiKey)
	files, err := storage.ReadDeletionFilesContext(ctx, current.UserId, current.Id)
	if err != nil {
		return err
	}
	if err := storage.DeleteFilesContext(ctx, files, true); err != nil {
		return err
	}
	database.DeleteFileRequest(current)
	return nil
}

// GetAllFiles returns a list of all files associated with a file request
func GetAllFiles(request models.FileRequest) []models.File {
	var result []models.File
	files := database.GetAllMetadata()
	for _, file := range files {
		if file.UploadRequestId == request.Id {
			result = append(result, file)
		}
	}
	return result
}
