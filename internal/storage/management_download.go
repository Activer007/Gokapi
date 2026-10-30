package storage

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/authorization"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
	"github.com/Activer007/SupaShare/internal/webserver/headers"
	"github.com/Activer007/SupaShare/internal/webserver/sse"
)

var ErrDownloadUnavailable = errors.New("download unavailable")

// ManagementDownload holds files alive through a transfer (and a bounded retry
// window). Only collection files consume an allowance; ordinary management
// downloads retain their existing behavior.
type ManagementDownload struct {
	Files   []models.File
	Charged bool
	guards  []string
	once    sync.Once
}

func (d *ManagementDownload) Close() {
	d.once.Do(func() {
		for _, guard := range d.guards {
			downloadstatus.SetComplete(guard)
		}
	})
}

// ValidateManagementRange accepts a single satisfiable range for seekable local
// files. Generated ZIPs and decrypted streams can be retried in full instead.
// This runs before claiming a capability or consuming any allowance.
func ValidateManagementRange(r *http.Request, files []models.File) bool {
	if r.Method == http.MethodHead {
		return true
	}
	rangeValue := r.Header.Get("Range")
	if rangeValue == "" {
		return true
	}
	if len(files) != 1 || files[0].Encryption.IsEncrypted || !files[0].IsLocalStorage() || len(rangeValue) > 128 || !strings.HasPrefix(rangeValue, "bytes=") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(rangeValue, "bytes="), "-")
	if len(parts) != 2 || files[0].SizeBytes <= 0 {
		return false
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		return err == nil && suffix > 0
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= files[0].SizeBytes {
		return false
	}
	if parts[1] == "" {
		return true
	}
	end, err := strconv.ParseInt(parts[1], 10, 64)
	return err == nil && end >= start
}

func (d *ManagementDownload) Serve(w http.ResponseWriter, r *http.Request, filename string) {
	// Conditional cache probes must not turn a newly charged transfer into a
	// bodyless 304/412 response. Management capabilities are non-cacheable.
	r = r.Clone(r.Context())
	for _, name := range []string{"If-Modified-Since", "If-Unmodified-Since", "If-Match", "If-None-Match", "If-Range"} {
		r.Header.Del(name)
	}
	if r.Method == http.MethodHead {
		if len(d.Files) == 1 {
			headers.Write(d.Files[0], w, true, false)
		} else {
			w.Header().Set("Content-Type", "application/zip")
		}
		return
	}
	if len(d.Files) == 1 {
		file := d.Files[0]
		ServeFile(file, w, r, true, false, file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted, false)
		return
	}
	ServeFilesAsZip(d.Files, filename, w, r)
}

// PrepareManagementDownload rechecks the current actor and every file under the
// metadata locks before charging any ZIP member. A retry can bypass exhausted
// allowances only when its server-held grant already charged this same snapshot.
func PrepareManagementDownload(ctx context.Context, snapshots []models.File, actorID int, retryCharged, count bool) (*ManagementDownload, error) {
	ids := make([]string, 0, len(snapshots))
	seen := make(map[string]bool)
	for _, file := range snapshots {
		if file.Id == "" || seen[file.Id] {
			return nil, ErrDownloadUnavailable
		}
		seen[file.Id] = true
		ids = append(ids, file.Id)
	}
	if len(ids) == 0 {
		return nil, ErrDownloadUnavailable
	}
	unlock, err := apimutex.LockManyContext(ctx, apimutex.TypeMetaData, ids...)
	if err != nil {
		return nil, err
	}
	defer unlock()
	actor, ok := database.GetUser(actorID)
	if !ok {
		return nil, ErrDownloadUnavailable
	}
	download := &ManagementDownload{Charged: retryCharged}
	for _, snapshot := range snapshots {
		file, found := database.GetMetaDataById(snapshot.Id)
		if !found || !sameDownloadFile(snapshot, file) || file.IsPendingForDeletion() ||
			(!file.UnlimitedTime && file.ExpireAt < time.Now().Unix()) ||
			(!(retryCharged && file.IsFileRequest()) && !file.UnlimitedDownloads && file.DownloadsRemaining < 1) ||
			!authorization.Authorize(actor, authorization.ReadFile, file.UserId) ||
			!checkIfValidAws(file) || !FileExists(file, configuration.Get().DataDir) {
			return nil, ErrDownloadUnavailable
		}
		// Detect local read/key failures before consuming an allowance or sending
		// archive headers. Mid-transfer failures can use the same bounded grant.
		if file.IsLocalStorage() {
			handle, actualSize, err := getFileHandler(file, configuration.Get().DataDir)
			if err != nil {
				return nil, ErrDownloadUnavailable
			}
			expectedSize := file.SizeBytes
			if file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted && expectedSize >= 0 {
				// Server-encrypted metadata stores plaintext size. E2E metadata
				// already stores ciphertext size, so it must not gain overhead twice.
				expectedSize = encryption.CalculateEncryptedFilesize(expectedSize)
			}
			valid := file.SizeBytes >= 0 && expectedSize >= file.SizeBytes && actualSize == expectedSize
			if valid && file.Encryption.IsEncrypted && !file.RequiresClientDecryption() {
				valid = encryption.IsCorrectKey(file.Encryption, handle)
			}
			_ = handle.Close()
			if !valid {
				return nil, ErrDownloadUnavailable
			}
		}
		download.Files = append(download.Files, file)
	}
	// Register every ZIP member before reducing its remaining count to zero, so
	// background cleanup cannot retire a later member while the archive streams.
	for _, file := range download.Files {
		download.guards = append(download.guards, downloadstatus.SetDownload(file))
	}
	if count && !actor.IsAdmin() && !retryCharged {
		for i, file := range download.Files {
			if !file.IsFileRequest() {
				continue
			}
			database.IncreaseDownloadCount(file.Id, !file.UnlimitedDownloads)
			file.DownloadCount++
			if !file.UnlimitedDownloads {
				file.DownloadsRemaining--
			}
			download.Files[i] = file
			go sse.PublishDownloadCount(file)
			download.Charged = true
		}
	}
	return download, nil
}

func sameDownloadFile(a, b models.File) bool {
	return a.Id == b.Id && a.UserId == b.UserId && a.UploadRequestId == b.UploadRequestId &&
		a.SHA1 == b.SHA1 && a.AwsBucket == b.AwsBucket && a.SizeBytes == b.SizeBytes &&
		a.Encryption.IsEncrypted == b.Encryption.IsEncrypted && a.Encryption.IsEndToEndEncrypted == b.Encryption.IsEndToEndEncrypted &&
		bytes.Equal(a.Encryption.DecryptionKey, b.Encryption.DecryptionKey) && bytes.Equal(a.Encryption.Nonce, b.Encryption.Nonce)
}
