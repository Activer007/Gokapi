package uploadquota

import (
	"bytes"

	"github.com/Activer007/SupaShare/internal/models"
)

// CheckDuplicateSource compares the security-relevant projection read on the
// provider's transaction connection with the previously authorized snapshot.
// Download counters can advance, but exhausted/expired/deleted sources cannot
// be revived by a stale copy. No credential or encryption material is logged.
func CheckDuplicateSource(expected, current models.File, now int64) error {
	if now <= 0 || !validIdentity(Reservation{ID: expected.Id, UserID: expected.UserId, RequestID: expected.UploadRequestId}) || expected.SizeBytes < 0 ||
		expected.SHA1 == "" || len(expected.SHA1) > MaxIDLength || len(expected.AwsBucket) > 1024 ||
		len(expected.Encryption.DecryptionKey) > 128 || len(expected.Encryption.Nonce) > 128 {
		return ErrInvalid
	}
	if current.Id != expected.Id || current.UserId != expected.UserId || current.UploadRequestId != expected.UploadRequestId ||
		current.SizeBytes != expected.SizeBytes || current.SHA1 != expected.SHA1 || current.AwsBucket != expected.AwsBucket ||
		current.Encryption.IsEncrypted != expected.Encryption.IsEncrypted || current.Encryption.IsEndToEndEncrypted != expected.Encryption.IsEndToEndEncrypted ||
		!bytes.Equal(current.Encryption.DecryptionKey, expected.Encryption.DecryptionKey) || !bytes.Equal(current.Encryption.Nonce, expected.Encryption.Nonce) ||
		current.PendingDeletion != 0 || (!current.UnlimitedTime && current.ExpireAt < now) || (!current.UnlimitedDownloads && current.DownloadsRemaining < 1) {
		return ErrConflict
	}
	return nil
}

func duplicateMetadata(ledger Ledger, files map[string]models.File, change Change) (Ledger, Reservation, *models.File, error) {
	if change.Source == nil || change.File == nil || !change.Upload.BytesKnown || change.Upload.StagingBytes != 0 {
		return rejected(ErrInvalid)
	}
	source, file := *change.Source, *change.File
	requestID := source.UploadRequestId
	if change.DetachCollection {
		if requestID == "" || file.UploadRequestId != "" || change.CheckRequestPolicy {
			return rejected(ErrConflict)
		}
		requestID = ""
	}
	current, found := files[source.Id]
	if !found || current.UserId != source.UserId || current.SizeBytes != source.SizeBytes || current.UploadRequestId != source.UploadRequestId ||
		file.Id != change.Upload.ID || file.Id == source.Id || file.UserId != change.Upload.UserID || file.UploadRequestId != requestID ||
		file.UploadRequestId != change.Upload.RequestID || file.SizeBytes != source.SizeBytes || file.SizeBytes != change.Upload.Bytes ||
		file.SHA1 != source.SHA1 || file.AwsBucket != source.AwsBucket || file.Encryption.IsEncrypted != source.Encryption.IsEncrypted ||
		file.Encryption.IsEndToEndEncrypted != source.Encryption.IsEndToEndEncrypted || !bytes.Equal(file.Encryption.DecryptionKey, source.Encryption.DecryptionKey) || !bytes.Equal(file.Encryption.Nonce, source.Encryption.Nonce) {
		return rejected(ErrConflict)
	}
	if _, exists := files[file.Id]; exists {
		return rejected(ErrConflict)
	}
	for _, record := range ledger.Records {
		if record.FileID == file.Id {
			return rejected(ErrConflict)
		}
	}
	// An instantaneous insert must count every outstanding reservation, even
	// if a deliberately colliding ID was supplied. It consumes no upload slot.
	candidate := change.Upload
	candidate.ID = ""
	if err := checkCapacityMode(ledger, files, candidate, change.Limits, true); err != nil {
		return rejected(err)
	}
	return ledger, Reservation{}, &file, nil
}
