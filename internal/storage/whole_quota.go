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

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/environment"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/chunking"
	"github.com/Activer007/SupaShare/internal/storage/filesystem/s3filesystem/aws"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// NewFileReserved admits a received whole file through durable quota accounting.
// closeInput must stop and clean up the caller-owned reception spool.
func NewFileReserved(ctx context.Context, content io.Reader, header *multipart.FileHeader, owner int, params models.UploadParameters, limits uploadquota.Limits, closeInput func() error) (models.File, error) {
	return newFileReserved(ctx, content, header, owner, params, limits, closeInput, database.ApplyUploadQuota)
}

func newFileReserved(ctx context.Context, content io.Reader, header *multipart.FileHeader, owner int, params models.UploadParameters, limits uploadquota.Limits, closeInput func() error, apply func(context.Context, uploadquota.Change) (uploadquota.Reservation, error)) (result models.File, err error) {
	return finishReservedFile(ctx, content, header, owner, params, limits, uploadquota.Reservation{}, closeInput, apply)
}

// NewFileClaimed completes an already frozen and claimed chunk upload. It must
// not create another reservation or choose a second metadata identity.
func NewFileClaimed(ctx context.Context, content io.Reader, header *multipart.FileHeader, owner int, params models.UploadParameters, limits uploadquota.Limits, upload uploadquota.Reservation, closeInput func() error) (models.File, error) {
	if upload.ID == "" {
		if closeInput != nil {
			return models.File{}, errors.Join(uploadquota.ErrInvalid, closeInput())
		}
		return models.File{}, uploadquota.ErrInvalid
	}
	return finishReservedFile(ctx, content, header, owner, params, limits, upload, closeInput, database.ApplyUploadQuota)
}

func finishReservedFile(ctx context.Context, content io.Reader, header *multipart.FileHeader, owner int, params models.UploadParameters, limits uploadquota.Limits, initial uploadquota.Reservation, closeInput func() error, apply func(context.Context, uploadquota.Change) (uploadquota.Reservation, error)) (result models.File, err error) {
	if closeInput == nil {
		return models.File{}, uploadquota.ErrInvalid
	}
	if ctx == nil || apply == nil {
		return models.File{}, errors.Join(uploadquota.ErrInvalid, closeInput())
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
	if content == nil || header == nil || owner <= 0 || header.Size < 0 || header.Size > (math.MaxInt64-(64<<10))/4 {
		return models.File{}, uploadquota.ErrInvalid
	}
	if initial.ID != "" {
		if initial.UserID != owner || initial.RequestID != params.FileRequestId || !initial.BytesKnown || initial.Bytes != header.Size || !initial.Completing || initial.FileID == "" || initial.State != uploadquota.Uploading {
			return models.File{}, uploadquota.ErrConflict
		}
		upload, reserved = initial, true
	} else if params.FileRequestId != "" {
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
	if !reserved {
		now := time.Now().Unix()
		upload = uploadquota.Reservation{ID: "whole-" + rand.Text(), UserID: owner,
			BytesKnown: true, Bytes: header.Size, StagingBytes: 4*header.Size + (64 << 10), ExpiresAt: now + uploadquota.MaxLifetime}
		if _, err := apply(ctx, uploadquota.Change{Action: uploadquota.Reserve, Upload: upload, Limits: limits, Now: now}); err != nil {
			// A failed reply may have created a reservation. Do not retry it
			// under a fresh identity or assume that capacity was refunded.
			return models.File{}, err
		}
		reserved = true
		upload.FileID = createNewId()
		if _, err := apply(ctx, uploadquota.Change{Action: uploadquota.Claim, Upload: upload, FileID: upload.FileID, Now: time.Now().Unix()}); err != nil {
			return models.File{}, err
		}
	}
	if err := checkProcessingSpace(ctx, header.Size); err != nil {
		return models.File{}, err
	}
	hash, encInfo, err := work.prepare(ctx, content, header.Size, params.IsEndToEndEncrypted)
	if err != nil {
		return models.File{}, err
	}
	file := buildNewMetaData(upload.FileID, hash, parsed, owner, params)
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
	if _, err := apply(ctx, uploadquota.Change{Action: uploadquota.Commit, Upload: upload, File: &file, Limits: limits, Now: time.Now().Unix(), CheckRequestPolicy: upload.RequestID != ""}); err != nil {
		return models.File{}, err
	}
	committed = true
	return file, nil
}

// This is a conservative free-space checkpoint, independent of logical quota.
// It is not an atomic filesystem allocator against other processes or writers.
func checkProcessingSpace(ctx context.Context, bytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	need, err := processingSpaceRequired(bytes, environment.New().MinFreeSpaceMB)
	if err != nil {
		return err
	}
	free, err := helper.GetFreeSpace(configuration.Get().DataDir)
	if err != nil {
		return err
	}
	if free < need {
		return uploadquota.ErrStaging
	}
	return nil
}

func processingSpaceRequired(bytes int64, floorMiB int) (uint64, error) {
	if bytes < 0 || bytes > (math.MaxInt64-(64<<10))/3 || floorMiB < 0 || int64(floorMiB) > math.MaxInt64/(1<<20) {
		return 0, uploadquota.ErrInvalid
	}
	// Plaintext and ciphertext can coexist during preparation. The stream's
	// authentication overhead grows with size; a fixed 64KiB alone is insufficient.
	cipherBytes := encryption.CalculateEncryptedFilesize(bytes)
	if cipherBytes < bytes || cipherBytes > math.MaxInt64-bytes-(64<<10) {
		return 0, uploadquota.ErrInvalid
	}
	need, floor := bytes+cipherBytes+(64<<10), int64(floorMiB)*(1<<20)
	if floor > math.MaxInt64-need {
		return 0, uploadquota.ErrInvalid
	}
	return uint64(need + floor), nil
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
