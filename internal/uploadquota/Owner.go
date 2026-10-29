package uploadquota

import "github.com/Activer007/SupaShare/internal/models"

// transferOwner admits only the receiving user's positive logical delta.
// Providers must patch UserId in the same transaction, preserving all other
// current fields; the returned nil metadata must never replace a stored file.
func transferOwner(ledger Ledger, files map[string]models.File, change Change) (Ledger, Reservation, *models.File, error) {
	if change.Source == nil || change.File != nil || change.FileID != "" || change.Upload.BytesKnown || change.Upload.Bytes != 0 || change.Upload.StagingBytes != 0 {
		return rejected(ErrInvalid)
	}
	source := *change.Source
	if source.Id != change.Upload.ID || source.SizeBytes < 0 || !validIdentity(Reservation{ID: source.Id, UserID: source.UserId, RequestID: source.UploadRequestId}) {
		return rejected(ErrInvalid)
	}
	current, found := files[source.Id]
	if !found || current.Id != source.Id || current.UserId != source.UserId || current.SizeBytes != source.SizeBytes || current.UploadRequestId != source.UploadRequestId {
		return rejected(ErrConflict)
	}
	if current.UserId == change.Upload.UserID {
		// No logical change: an existing over-limit owner can still retry.
		return ledger, Reservation{}, nil, nil
	}
	snapshot, err := Measure(files)
	if err != nil {
		return rejected(ErrInvalid)
	}
	var reserved Usage
	for _, record := range ledger.Records {
		if !isActive(record) || record.UserID != change.Upload.UserID {
			continue
		}
		usage := Usage{Files: 1}
		if record.BytesKnown {
			usage.Bytes = record.Bytes
		}
		reserved, err = add(reserved, usage)
		if err != nil {
			return rejected(ErrInvalid)
		}
	}
	allowed, err := CanReserve(snapshot.Users[change.Upload.UserID], reserved, Usage{Bytes: current.SizeBytes, Files: 1}, change.Limits.User)
	if err != nil {
		return rejected(ErrInvalid)
	}
	if !allowed {
		return rejected(ErrQuota)
	}
	return ledger, Reservation{}, nil, nil
}
