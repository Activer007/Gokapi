package filerequest

import (
	"context"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/uploadquota"
)

// New creates a new file request object. It is not stored yet,
// and an API key has to be generated manually
func New(user models.User) models.FileRequest {
	return models.FileRequest{
		Id:           helper.GenerateRandomString(configuration.GetEnvironment().LengthId),
		UserId:       user.Id,
		CreationDate: time.Now().Unix(),
		Name:         "Unnamed file request",
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
	files := GetAllFiles(request)
	storage.DeleteFiles(files, true)
	database.DeleteFileRequest(request)
	database.DeleteApiKey(request.ApiKey)
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
