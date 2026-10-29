package uploadquota

import "github.com/Activer007/SupaShare/internal/models"

// MaxMetadataTextBytes bounds replacement text copied from a trusted snapshot.
const MaxMetadataTextBytes = 64 << 10

// ValidateReplacement checks the transient command before providers read or
// copy display fields. The current security projection is rechecked in TX/CAS.
func ValidateReplacement(change Change) error {
	if change.Action != ReplaceMetadata || change.Source == nil || change.Target == nil || change.File != nil || change.FileID != "" ||
		change.Upload.BytesKnown || change.Upload.Bytes != 0 || change.Upload.StagingBytes != 0 || change.Upload.RequestID != "" ||
		change.Upload.ID != change.Target.Id || change.Upload.UserID != change.Target.UserId {
		return ErrInvalid
	}
	target, source := *change.Target, *change.Source
	if target.UploadRequestId != "" || target.Encryption.IsEndToEndEncrypted || source.Encryption.IsEndToEndEncrypted ||
		(change.DeleteSource && target.Id == source.Id) {
		return ErrConflict
	}
	if err := CheckDuplicateSource(target, target, change.Now); err != nil {
		return err
	}
	if err := CheckDuplicateSource(source, source, change.Now); err != nil {
		return err
	}
	for _, value := range []string{source.Name, source.Size, source.ContentType} {
		if len(value) > MaxMetadataTextBytes {
			return ErrInvalid
		}
	}
	return nil
}

func replaceMetadata(ledger Ledger, files map[string]models.File, change Change) (Ledger, Reservation, *models.File, error) {
	if err := ValidateReplacement(change); err != nil {
		return rejected(err)
	}
	target, source := *change.Target, *change.Source
	for _, expected := range []models.File{target, source} {
		current, found := files[expected.Id]
		if !found || current.Id != expected.Id || current.UserId != expected.UserId || current.SizeBytes != expected.SizeBytes || current.UploadRequestId != expected.UploadRequestId {
			return rejected(ErrConflict)
		}
	}
	if source.SizeBytes <= target.SizeBytes {
		// No growth: existing over-limit usage must still be able to shrink.
		return ledger, Reservation{}, nil, nil
	}
	snapshot, err := Measure(files)
	if err != nil {
		return rejected(ErrInvalid)
	}
	var global, reserved, userReserved Usage
	for _, usage := range snapshot.Users {
		global, err = add(global, Usage{Bytes: usage.Bytes})
		if err != nil {
			return rejected(ErrInvalid)
		}
	}
	for _, record := range ledger.Records {
		if !isActive(record) || !record.BytesKnown {
			continue
		}
		reserved, err = add(reserved, Usage{Bytes: record.Bytes})
		if err != nil {
			return rejected(ErrInvalid)
		}
		if record.UserID == target.UserId {
			userReserved, err = add(userReserved, Usage{Bytes: record.Bytes})
			if err != nil {
				return rejected(ErrInvalid)
			}
		}
	}
	delta := Usage{Bytes: source.SizeBytes - target.SizeBytes}
	for _, check := range []struct{ used, held, capacity Usage }{
		{Usage{Bytes: snapshot.Users[target.UserId].Bytes}, userReserved, Usage{Bytes: change.Limits.User.Bytes}},
		{global, reserved, Usage{Bytes: change.Limits.Instance.Bytes}},
	} {
		allowed, err := CanReserve(check.used, check.held, delta, check.capacity)
		if err != nil {
			return rejected(ErrInvalid)
		}
		if !allowed {
			return rejected(ErrQuota)
		}
	}
	return ledger, Reservation{}, nil, nil
}
