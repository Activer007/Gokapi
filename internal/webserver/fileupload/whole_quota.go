package fileupload

import (
	"context"
	"errors"
	"net/http"

	"github.com/forceu/gokapi/internal/environment"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/uploadquota"
	"github.com/forceu/gokapi/internal/webserver/errorHandling/errorcodes"
	"github.com/forceu/gokapi/internal/webserver/fileupload/uploadservice"
)

func wholeUploadLimits(env environment.Environment) (uploadquota.Limits, error) {
	if err := env.ValidateWholeUploadLimits(); err != nil {
		return uploadquota.Limits{}, err
	}
	const mib = 1024 * 1024
	return uploadquota.Limits{
		User:       uploadquota.Usage{Bytes: env.UploadUserTotalMB * mib, Files: env.UploadUserMaxFiles},
		Instance:   uploadquota.Usage{Bytes: env.UploadInstanceTotalMB * mib, Files: env.UploadInstanceMaxFiles},
		UserActive: env.UploadUserActive, InstanceActive: env.UploadInstanceActive,
		StagingBytes: env.UploadStagingMB * mib,
	}, nil
}

// Only errors from the new durable storage path are opaque. Earlier multipart
// and parameter validation keeps its existing public error contract.
type wholeUploadFailure struct{ error }

func (err wholeUploadFailure) Unwrap() error { return err.error }

func (err wholeUploadFailure) Error() string {
	_, _, message := WholeUploadErrorResponse(err)
	return message
}

// WholeUploadErrorResponse never exposes provider errors, storage paths,
// encryption material or internal identifiers from the durable upload path.
func WholeUploadErrorResponse(err error) (int, int, string) {
	var failure wholeUploadFailure
	if !errors.As(err, &failure) {
		return ReceiveErrorStatus(err), ReceiveErrorCode(err, errorcodes.UnspecifiedError), err.Error()
	}
	switch {
	case errors.Is(err, uploadquota.ErrActive):
		return http.StatusTooManyRequests, errorcodes.UploadQuotaBusy, "Upload capacity is busy; retry later"
	case errors.Is(err, uploadquota.ErrStaging):
		return http.StatusInsufficientStorage, errorcodes.CannotAllocateFile, "Upload staging capacity is unavailable"
	case errors.Is(err, uploadquota.ErrQuota):
		return http.StatusRequestEntityTooLarge, errorcodes.UploadQuotaExceeded, "Upload quota exceeded"
	case errors.Is(err, storage.ErrorFileTooLarge):
		return http.StatusRequestEntityTooLarge, errorcodes.FileTooLarge, "Upload limit exceeded"
	case errors.Is(err, uploadquota.ErrFull), errors.Is(err, uploadquota.ErrRetry):
		return http.StatusServiceUnavailable, errorcodes.UploadQuotaBusy, "Upload capacity is busy; retry later"
	case errors.Is(err, uploadservice.ErrClosing):
		return http.StatusServiceUnavailable, errorcodes.UploadQuotaBusy, "Upload service is shutting down"
	case errors.Is(err, uploadquota.ErrExpired):
		return http.StatusGone, errorcodes.InvalidChunkReservation, "Upload reservation has expired"
	case errors.Is(err, uploadquota.ErrNotFound):
		return http.StatusBadRequest, errorcodes.InvalidChunkReservation, "Upload reservation was not found"
	case errors.Is(err, uploadquota.ErrConflict), errors.Is(err, uploadquota.ErrInProgress):
		return http.StatusConflict, errorcodes.ResourceCanNotBeEdited, "Upload could not be completed"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout, errorcodes.UnspecifiedError, "Upload was interrupted"
	default:
		return http.StatusInternalServerError, errorcodes.InternalServer, "Upload could not be completed"
	}
}
