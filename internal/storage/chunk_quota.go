package storage

import (
	"context"
	"math"
	"mime"
	"mime/multipart"
	"net/textproto"
	"path/filepath"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/chunking"
	"github.com/Activer007/SupaShare/internal/storage/chunking/chunksession"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

var chunkStoreCache struct {
	sync.Mutex
	root  string
	store *chunksession.Store
}

// ChunkSessions returns one store per current DataDir. Production configuration
// is fixed while serving; isolated tests replace DataDir only after draining I/O.
func ChunkSessions() *chunksession.Store {
	chunkStoreCache.Lock()
	defer chunkStoreCache.Unlock()
	root := configuration.Get().DataDir
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	if chunkStoreCache.store == nil || chunkStoreCache.root != root {
		chunkStoreCache.root, chunkStoreCache.store = root, chunksession.New(root)
	}
	return chunkStoreCache.store
}

// ChunkCompletion holds the frozen input and claim until Run completes cleanup.
// Creating it is synchronous, before any asynchronous task is launched.
type ChunkCompletion struct {
	lease  *chunksession.Lease
	header chunking.FileHeader
	owner  int
	params models.UploadParameters
	limits uploadquota.Limits
}

func PrepareChunkFile(ctx context.Context, clientID string, header chunking.FileHeader, owner int, params models.UploadParameters, limits uploadquota.Limits) (*ChunkCompletion, error) {
	if ctx == nil || owner <= 0 || header.Size < 0 {
		return nil, uploadquota.ErrInvalid
	}
	header.Filename = helper.SanitiseFilename(header.Filename)
	if header.Filename == "" {
		return nil, uploadquota.ErrInvalid
	}
	if header.ContentType == "" {
		header.ContentType = mime.TypeByExtension(filepath.Ext(header.Filename))
		if header.ContentType == "" {
			header.ContentType = "application/octet-stream"
		}
	}
	header.ContentType = helper.SanitiseContentType(header.ContentType)
	level := configuration.Get().Encryption.Level
	if level < encryption.NoEncryption || level > encryption.EndToEndEncryption || (params.IsEndToEndEncrypted && isEncryptionRequested()) {
		return nil, uploadquota.ErrInvalid
	}
	maxMiB := configuration.Get().MaxFileSizeMB
	if maxMiB <= 0 || int64(maxMiB) > math.MaxInt64/(1<<20) {
		return nil, uploadquota.ErrInvalid
	}
	if header.Size > int64(maxMiB)*(1<<20) {
		return nil, ErrorFileTooLarge
	}
	lease, err := ChunkSessions().Prepare(ctx, chunksession.Scope{ClientID: clientID, UserID: owner, RequestID: params.FileRequestId}, header, createNewId(), limits)
	if err != nil {
		return nil, err
	}
	return &ChunkCompletion{lease: lease, header: header, owner: owner, params: params, limits: limits}, nil
}

func (completion *ChunkCompletion) AlreadyCommitted() bool { return completion.lease.AlreadyCommitted }

func (completion *ChunkCompletion) Deadline() time.Time {
	if completion.AlreadyCommitted() {
		// A retained receipt may outlive its original active-upload deadline.
		return time.Now().Add(5 * time.Second)
	}
	return time.Unix(completion.lease.Upload.ExpiresAt, 0)
}

func (completion *ChunkCompletion) Run(ctx context.Context) (models.File, error) {
	defer completion.lease.Release()
	if completion.AlreadyCommitted() {
		if ctx == nil {
			return models.File{}, uploadquota.ErrInvalid
		}
		if err := ctx.Err(); err != nil {
			return models.File{}, err
		}
		file, ok := database.GetMetaDataById(completion.lease.Upload.FileID)
		if !ok || file.UserId != completion.owner || file.UploadRequestId != completion.params.FileRequestId {
			return models.File{}, uploadquota.ErrConflict
		}
		// Replays never recreate deleted metadata or release unknown old work.
		return file, nil
	}
	header := &multipart.FileHeader{Filename: completion.header.Filename, Size: completion.header.Size, Header: make(textproto.MIMEHeader)}
	header.Header.Set("Content-Type", completion.header.ContentType)
	return NewFileClaimed(ctx, completion.lease.Input, header, completion.owner, completion.params, completion.limits, completion.lease.Upload, completion.lease.CloseInput)
}
