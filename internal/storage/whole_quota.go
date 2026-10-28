package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"io"
	"math"
	"mime/multipart"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/encryption"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/chunking"
	"github.com/forceu/gokapi/internal/storage/filesystem/s3filesystem/aws"
	"github.com/forceu/gokapi/internal/uploadquota"
)

// NewFileReserved admits a received whole file through durable quota accounting.
// closeInput must stop and clean up the caller-owned reception spool.
func NewFileReserved(ctx context.Context, content io.Reader, header *multipart.FileHeader, owner int, params models.UploadParameters, limits uploadquota.Limits, closeInput func() error) (models.File, error) {
	return newFileReserved(ctx, content, header, owner, params, limits, closeInput, database.ApplyUploadQuota)
}

func newFileReserved(ctx context.Context, content io.Reader, header *multipart.FileHeader, owner int, params models.UploadParameters, limits uploadquota.Limits, closeInput func() error, apply func(context.Context, uploadquota.Change) (uploadquota.Reservation, error)) (result models.File, err error) {
	if closeInput == nil {
		return models.File{}, uploadquota.ErrInvalid
	}
	work := &wholeWork{}
	var upload uploadquota.Reservation
	reserved, published, commitAttempted, committed := false, false, false, false
	defer func() {
		// All work is synchronous and has stopped before this cleanup. A close
		// failure is sticky: neither its path nor its charge is refunded.
		cleanupErr := errors.Join(work.cleanup(), closeInput())
		if reserved && cleanupErr == nil && (committed || (!published && !commitAttempted)) {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			change := uploadquota.Change{Upload: upload, Now: time.Now().Unix(), CleanupConfirmed: true}
			if committed {
				change.Action = uploadquota.ReleaseStaging
			} else {
				change.Action, change.Terminal = uploadquota.Abort, uploadquota.Aborted
			}
			_, cleanupErr = apply(cleanupCtx, change)
		}
		if cleanupErr != nil {
			result, err = models.File{}, errors.Join(err, cleanupErr)
		}
	}()
	if ctx == nil || content == nil || header == nil || owner <= 0 || apply == nil || params.FileRequestId != "" || header.Size < 0 || header.Size > (math.MaxInt64-(64<<10))/4 {
		return models.File{}, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, err
	}
	level := configuration.Get().Encryption.Level
	if level < encryption.NoEncryption || level > encryption.EndToEndEncryption || (params.IsEndToEndEncrypted && isEncryptionRequested()) {
		// A client flag cannot disable required server encryption, and one
		// EncryptionInfo cannot describe both encryption layers faithfully.
		return models.File{}, uploadquota.ErrInvalid
	}
	maxMiB := configuration.Get().MaxFileSizeMB
	if maxMiB <= 0 || int64(maxMiB) > math.MaxInt64/(1<<20) {
		return models.File{}, uploadquota.ErrInvalid
	}
	if header.Size > int64(maxMiB)*(1<<20) {
		return models.File{}, ErrorFileTooLarge
	}
	parsed, err := chunking.ParseMultipartHeader(header)
	if err != nil {
		return models.File{}, err
	}
	now := time.Now().Unix()
	upload = uploadquota.Reservation{ID: "whole-" + rand.Text(), UserID: owner,
		BytesKnown: true, Bytes: header.Size, StagingBytes: 4*header.Size + (64 << 10), ExpiresAt: now + uploadquota.MaxLifetime}
	if _, err := apply(ctx, uploadquota.Change{Action: uploadquota.Reserve, Upload: upload, Limits: limits, Now: now}); err != nil {
		// A failed provider reply may have created a reservation. It must not
		// be silently retried under a new ID or assumed absent/refunded.
		return models.File{}, err
	}
	reserved = true
	fileID := createNewId()
	if _, err := apply(ctx, uploadquota.Change{Action: uploadquota.Claim, Upload: upload, FileID: fileID, Now: time.Now().Unix()}); err != nil {
		return models.File{}, err
	}
	hash, encInfo, err := work.prepare(ctx, content, header.Size, params.IsEndToEndEncrypted)
	if err != nil {
		return models.File{}, err
	}
	file := buildNewMetaData(fileID, hash, parsed, owner, params)
	file.Encryption = encInfo
	unlock, err := lockBlobPublication(ctx, file)
	if err != nil {
		return models.File{}, err
	}
	defer unlock()
	exists, size, err := probeWholeBlob(ctx, file)
	if err != nil {
		return models.File{}, err
	}
	if exists {
		if size != work.size {
			return models.File{}, uploadquota.ErrConflict
		}
		previous, found, err := database.ReadBlobEncryption(ctx, file.SHA1, file.AwsBucket)
		if err != nil {
			return models.File{}, err
		}
		if found {
			if previous.IsEncrypted != file.Encryption.IsEncrypted || previous.IsEndToEndEncrypted != file.Encryption.IsEndToEndEncrypted {
				return models.File{}, uploadquota.ErrConflict
			}
			file.Encryption = previous
		} else if file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted {
			return models.File{}, uploadquota.ErrConflict
		}
	} else {
		if err := ctx.Err(); err != nil {
			return models.File{}, err
		}
		if file.IsLocalStorage() {
			if err := work.current.close(); err != nil {
				return models.File{}, err
			}
			published = true
			// Link is atomic and cannot replace an existing target. The owned
			// work link stays available for explicit post-Commit cleanup.
			err = os.Link(work.current.path, filepath.Join(configuration.Get().DataDir, file.SHA1))
		} else {
			published = true
			_, err = aws.UploadContext(ctx, work.current.file, file)
		}
		if err != nil {
			return models.File{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, err
	}
	setHotlinkCandidate(&file)
	commitAttempted = true
	if _, err := apply(ctx, uploadquota.Change{Action: uploadquota.Commit, Upload: upload, File: &file, Limits: limits, Now: time.Now().Unix()}); err != nil {
		return models.File{}, err
	}
	committed = true
	return file, nil
}

// Publication and reference changes use finite stripes shared by legacy whole
// files, chunks, duplication/replacement and GC. Expensive preparation precedes
// the lock. A cancelled request does not wait indefinitely for another writer.
var wholeBlobLocks struct {
	once  sync.Once
	locks [256]chan struct{}
}

func lockBlobPublication(ctx context.Context, file models.File) (func(), error) {
	wholeBlobLocks.once.Do(func() {
		for i := range wholeBlobLocks.locks {
			wholeBlobLocks.locks[i] = make(chan struct{}, 1)
		}
	})
	h := fnv.New32a()
	_, _ = h.Write([]byte(file.AwsBucket + "\x00" + file.SHA1))
	lock := wholeBlobLocks.locks[h.Sum32()%uint32(len(wholeBlobLocks.locks))]
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func probeWholeBlob(ctx context.Context, file models.File) (bool, int64, error) {
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	if !file.IsLocalStorage() {
		return aws.FileExistsContext(ctx, file)
	}
	info, err := os.Lstat(filepath.Join(configuration.Get().DataDir, file.SHA1))
	if errors.Is(err, os.ErrNotExist) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if !info.Mode().IsRegular() {
		return false, 0, uploadquota.ErrInvalid
	}
	return true, info.Size(), nil
}

type wholeOwnedFile struct {
	file     *os.File
	path     string
	closed   bool
	closeErr error
}

func (file *wholeOwnedFile) close() error {
	if !file.closed {
		file.closed = true
		file.closeErr = file.file.Close()
	}
	return file.closeErr
}

func (file *wholeOwnedFile) cleanup() error {
	if err := file.close(); err != nil {
		return err
	}
	if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

type wholeWork struct {
	files   []*wholeOwnedFile
	current *wholeOwnedFile
	size    int64
}

func (work *wholeWork) create() (*wholeOwnedFile, error) {
	file, err := os.CreateTemp(configuration.Get().DataDir, ".gokapi-whole-")
	if err != nil {
		return nil, err
	}
	owned := &wholeOwnedFile{file: file, path: file.Name()}
	work.files = append(work.files, owned)
	return owned, nil
}

func (work *wholeWork) cleanup() error {
	var err error
	for _, file := range work.files {
		err = errors.Join(err, file.cleanup())
	}
	return err
}

func (work *wholeWork) prepare(ctx context.Context, input io.Reader, size int64, e2e bool) (string, models.EncryptionInfo, error) {
	plain, err := work.create()
	if err != nil {
		return "", models.EncryptionInfo{}, err
	}
	hash := sha1.New()
	n, err := io.Copy(io.MultiWriter(plain.file, hash), wholeContextReader{ctx, io.LimitReader(input, size+1)})
	if err != nil {
		return "", models.EncryptionInfo{}, err
	}
	if n != size {
		return "", models.EncryptionInfo{}, uploadquota.ErrInvalid
	}
	if _, err := plain.file.Seek(0, io.SeekStart); err != nil {
		return "", models.EncryptionInfo{}, err
	}
	work.current = plain
	encInfo := models.EncryptionInfo{IsEncrypted: e2e, IsEndToEndEncrypted: e2e}
	if !e2e && isEncryptionRequested() {
		if !encryption.IsDecryptionAvailable() {
			return "", models.EncryptionInfo{}, uploadquota.ErrInvalid
		}
		cipher, err := work.create()
		if err != nil {
			return "", models.EncryptionInfo{}, err
		}
		if err := encryption.Encrypt(&encInfo, wholeContextReader{ctx, plain.file}, cipher.file); err != nil {
			return "", models.EncryptionInfo{}, err
		}
		if err := plain.cleanup(); err != nil {
			return "", models.EncryptionInfo{}, err
		}
		hash.Write([]byte(configuration.Get().Authentication.SaltFiles))
		work.current = cipher
	}
	if err := ctx.Err(); err != nil {
		return "", models.EncryptionInfo{}, err
	}
	if err := work.current.file.Sync(); err != nil {
		return "", models.EncryptionInfo{}, err
	}
	if _, err := work.current.file.Seek(0, io.SeekStart); err != nil {
		return "", models.EncryptionInfo{}, err
	}
	info, err := work.current.file.Stat()
	if err != nil {
		return "", models.EncryptionInfo{}, err
	}
	work.size = info.Size()
	return hex.EncodeToString(hash.Sum(nil)), encInfo, nil
}

type wholeContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader wholeContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
