package fileupload

import (
	"context"
	"sync"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage"
	"github.com/forceu/gokapi/internal/storage/chunking"
	"github.com/forceu/gokapi/internal/storage/chunking/chunksession"
	"github.com/forceu/gokapi/internal/storage/processingstatus"
	"github.com/forceu/gokapi/internal/uploadquota"
	"github.com/forceu/gokapi/internal/webserver/fileupload/uploadservice"
)

func chunkUploadLimits() (uploadquota.Limits, error) {
	env := configuration.GetEnvironment()
	if err := env.ValidateRequestUploadLimits(); err != nil {
		return uploadquota.Limits{}, err
	}
	limits, err := wholeUploadLimits(env)
	if err != nil {
		return uploadquota.Limits{}, err
	}
	limits.Request.Bytes = env.UploadRequestTotalMB * (1 << 20)
	limits.RequestActive = env.UploadRequestActive
	return limits, nil
}

func ReserveFileRequestUpload(ctx context.Context, owner int, requestID string) (string, error) {
	limits, err := chunkUploadLimits()
	if err != nil {
		return "", wholeUploadFailure{err}
	}
	uuid := helper.GenerateRandomString(32)
	_, err = storage.ChunkSessions().Reserve(ctx, chunksession.Scope{ClientID: uuid, UserID: owner, RequestID: requestID}, limits)
	if err != nil {
		return "", wholeUploadFailure{err}
	}
	return uuid, nil
}

func CancelFileRequestUpload(ctx context.Context, owner int, requestID, uuid string) error {
	if err := storage.ChunkSessions().Cancel(ctx, chunksession.Scope{ClientID: uuid, UserID: owner, RequestID: requestID}); err != nil {
		return wholeUploadFailure{err}
	}
	return nil
}

// PreparedCompletion accounts for preparation and the whole caller-owned worker,
// including its logging and response tail. Successful preparation transfers the
// obligation to call Done to the caller, even when Run returns an error.
type PreparedCompletion struct {
	completion *storage.ChunkCompletion
	ticket     *uploadservice.Ticket
	uuid       string
	owner      int
	once       sync.Once
	file       models.File
	err        error
}

func PrepareChunkCompletion(ctx context.Context, uuid string, header chunking.FileHeader, owner int, params models.UploadParameters) (*PreparedCompletion, error) {
	ticket, err := uploadservice.Current().Acquire(ctx)
	if err != nil {
		return nil, wholeUploadFailure{err}
	}
	fail := func(err error) (*PreparedCompletion, error) {
		ticket.Done()
		return nil, wholeUploadFailure{err}
	}
	limits, err := chunkUploadLimits()
	if err != nil {
		return fail(err)
	}
	if params.FileRequestId != "" {
		user, ok := database.GetUser(owner)
		if !ok {
			return fail(uploadquota.ErrConflict)
		}
		env := configuration.GetEnvironment()
		if !user.IsAdmin() && env.MaxSizeGuestUploadMb != 0 {
			maximum, err := MaxUploadBytes(env.MaxSizeGuestUploadMb)
			if err != nil {
				return fail(err)
			}
			if header.Size > maximum {
				return fail(storage.ErrorFileTooLarge)
			}
		}
	}
	completion, err := storage.PrepareChunkFile(ticket.Context(), uuid, header, owner, params, limits)
	if err != nil {
		return fail(err)
	}
	return &PreparedCompletion{completion: completion, ticket: ticket, uuid: uuid, owner: owner}, nil
}

func (completion *PreparedCompletion) AlreadyCommitted() bool {
	return completion.completion.AlreadyCommitted()
}

func (completion *PreparedCompletion) Run() (models.File, error)         { return completion.run(false) }
func (completion *PreparedCompletion) RunDetached() (models.File, error) { return completion.run(true) }

// Done ends service drain accounting after the worker's last operation. It is
// idempotent; Run and RunDetached do not release the caller's lifetime ticket.
func (completion *PreparedCompletion) Done() { completion.ticket.Done() }

func (completion *PreparedCompletion) run(detached bool) (models.File, error) {
	completion.once.Do(func() {
		ctx := completion.ticket.Context()
		var cancel context.CancelFunc
		if detached {
			ctx, cancel = completion.ticket.Detached(completion.completion.Deadline())
		} else {
			ctx, cancel = context.WithDeadline(ctx, completion.completion.Deadline())
		}
		defer cancel()
		if !completion.AlreadyCommitted() {
			processingstatus.Set(completion.uuid, processingstatus.StatusHashingOrEncrypting, models.File{}, completion.owner, nil)
		}
		completion.file, completion.err = completion.completion.Run(ctx)
		if completion.err != nil {
			completion.err = wholeUploadFailure{completion.err}
			processingstatus.Set(completion.uuid, processingstatus.StatusError, models.File{}, completion.owner, completion.err)
		} else {
			processingstatus.SetCommitted(completion.uuid, completion.file, completion.owner)
		}
	})
	return completion.file, completion.err
}
