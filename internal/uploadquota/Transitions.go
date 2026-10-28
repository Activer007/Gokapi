package uploadquota

import (
	"fmt"
	"math"

	"github.com/forceu/gokapi/internal/models"
)

// Apply returns a replacement ledger and optional new metadata. Providers must
// atomically persist both; this function never performs database or file I/O.
func Apply(ledger Ledger, files map[string]models.File, change Change) (Ledger, Reservation, *models.File, error) {
	if err := validateLedger(ledger); err != nil {
		return rejected(err)
	}
	if change.Now <= 0 || change.Now > math.MaxInt64-MaxLifetime || !validIdentity(change.Upload) {
		return rejected(ErrInvalid)
	}
	next := EmptyLedger()
	for id, record := range ledger.Records {
		next.Records[id] = record
	}
	if change.Action == Reserve {
		for id, record := range next.Records {
			if !isActive(record) && record.StagingReleased && change.Now >= record.FinishedAt && change.Now-record.FinishedAt >= ReceiptLifetime {
				delete(next.Records, id)
			}
		}
	}
	r, exists := next.Records[change.Upload.ID]
	if exists && (r.UserID != change.Upload.UserID || r.RequestID != change.Upload.RequestID) {
		return rejected(ErrConflict)
	}
	var metadata *models.File
	switch change.Action {
	case Reserve:
		if exists {
			if r.InitiallyUnbound {
				if change.Upload.BytesKnown || change.Upload.Bytes != 0 || change.Upload.StagingBytes != 0 {
					return rejected(ErrConflict)
				}
			} else if !change.Upload.BytesKnown || r.Bytes != change.Upload.Bytes || r.StagingBytes != change.Upload.StagingBytes {
				return rejected(ErrConflict)
			}
			if isActive(r) && r.ExpiresAt <= change.Now {
				return rejected(ErrExpired)
			}
			return next, r, nil, nil
		}
		if !validDeclaration(change.Upload) || change.Upload.ExpiresAt <= change.Now || change.Upload.ExpiresAt-change.Now > MaxLifetime {
			return rejected(ErrInvalid)
		}
		if len(next.Records) >= MaxRecords {
			return rejected(ErrFull)
		}
		users, requests := 0, 0
		for _, record := range next.Records {
			if record.UserID == change.Upload.UserID {
				users++
			}
			if change.Upload.RequestID != "" && record.RequestID == change.Upload.RequestID {
				requests++
			}
		}
		if users >= MaxUserRecords || requests >= MaxRequestRecords {
			return rejected(ErrFull)
		}
		r = change.Upload
		r.InitiallyUnbound = !r.BytesKnown
		r.CreatedAt, r.FinishedAt, r.State = change.Now, 0, Reserved
		r.Completing, r.StagingReleased, r.FileID, r.CommitDigest = false, false, "", ""
		if err := checkCapacity(next, files, r, change.Limits); err != nil {
			return rejected(err)
		}
	case Bind:
		if !exists {
			return rejected(ErrNotFound)
		}
		if !isActive(r) || r.Completing {
			return rejected(ErrConflict)
		}
		if r.ExpiresAt <= change.Now {
			return rejected(ErrExpired)
		}
		if !validDeclaration(change.Upload) || !change.Upload.BytesKnown {
			return rejected(ErrInvalid)
		}
		if r.BytesKnown && (r.Bytes != change.Upload.Bytes || r.StagingBytes != change.Upload.StagingBytes) {
			return rejected(ErrConflict)
		}
		r.BytesKnown, r.Bytes, r.StagingBytes, r.State = true, change.Upload.Bytes, change.Upload.StagingBytes, Uploading
		if err := checkCapacity(next, files, r, change.Limits); err != nil {
			return rejected(err)
		}
	case Claim:
		if !exists {
			return rejected(ErrNotFound)
		}
		if !validID(change.FileID) {
			return rejected(ErrInvalid)
		}
		if r.State == Committed && r.FileID == change.FileID {
			return next, r, nil, nil
		}
		if !isActive(r) || !r.BytesKnown {
			return rejected(ErrConflict)
		}
		if r.ExpiresAt <= change.Now {
			return rejected(ErrExpired)
		}
		if r.Completing {
			if r.FileID != change.FileID {
				return rejected(ErrConflict)
			}
			return rejected(ErrInProgress)
		}
		if _, ok := files[change.FileID]; ok {
			return rejected(ErrConflict)
		}
		for id, record := range next.Records {
			if id != r.ID && record.FileID == change.FileID {
				return rejected(ErrConflict)
			}
		}
		r.Completing, r.FileID, r.State = true, change.FileID, Uploading
	case Commit:
		if !exists {
			return rejected(ErrNotFound)
		}
		if change.File == nil || change.File.Id != r.FileID || change.File.UserId != r.UserID || change.File.UploadRequestId != r.RequestID || change.File.SizeBytes != r.Bytes {
			return rejected(ErrConflict)
		}
		digest, err := metadataDigest(*change.File)
		if err != nil {
			return rejected(err)
		}
		if r.State == Committed {
			if r.CommitDigest != digest {
				return rejected(ErrConflict)
			}
			return next, r, nil, nil
		}
		if !isActive(r) || !r.Completing || !r.BytesKnown {
			return rejected(ErrConflict)
		}
		if r.ExpiresAt <= change.Now {
			return rejected(ErrExpired)
		}
		if _, ok := files[r.FileID]; ok {
			return rejected(ErrConflict)
		}
		if err := checkCapacity(next, files, r, change.Limits); err != nil {
			return rejected(err)
		}
		file := *change.File
		metadata = &file
		r.State, r.Completing, r.FinishedAt, r.CommitDigest = Committed, false, change.Now, digest
		r.StagingReleased = r.StagingBytes == 0
	case Abort:
		if !exists {
			return rejected(ErrNotFound)
		}
		if !change.CleanupConfirmed || (change.Terminal != Aborted && change.Terminal != Expired) {
			return rejected(ErrInvalid)
		}
		if r.State == change.Terminal && r.StagingReleased {
			return next, r, nil, nil
		}
		if !isActive(r) {
			return rejected(ErrConflict)
		}
		if change.Terminal == Expired && change.Now < r.ExpiresAt {
			return rejected(ErrConflict)
		}
		r.State, r.Completing, r.StagingReleased, r.FinishedAt = change.Terminal, false, true, change.Now
	case ReleaseStaging:
		if !exists {
			return rejected(ErrNotFound)
		}
		if !change.CleanupConfirmed || r.State != Committed {
			return rejected(ErrConflict)
		}
		r.StagingReleased = true
	default:
		return rejected(ErrInvalid)
	}
	next.Records[r.ID] = r
	if _, err := Encode(next); err != nil {
		return rejected(err)
	}
	return next, r, metadata, nil
}

func rejected(err error) (Ledger, Reservation, *models.File, error) {
	return Ledger{}, Reservation{}, nil, err
}

func checkCapacity(ledger Ledger, files map[string]models.File, candidate Reservation, limits Limits) error {
	for _, n := range []int64{limits.User.Bytes, limits.User.Files, limits.Request.Bytes, limits.Request.Files, limits.Instance.Bytes, limits.Instance.Files, limits.UserActive, limits.RequestActive, limits.InstanceActive, limits.StagingBytes} {
		if n < 0 {
			return ErrInvalid
		}
	}
	snapshot, err := Measure(files)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	var global, userReserved, requestReserved, allReserved Usage
	for _, usage := range snapshot.Users {
		global, err = add(global, usage)
		if err != nil {
			return ErrInvalid
		}
	}
	var userActive, requestActive, allActive, staging int64
	for id, r := range ledger.Records {
		if id == candidate.ID {
			continue
		}
		if isActive(r) {
			usage := Usage{Files: 1}
			if r.BytesKnown {
				usage.Bytes = r.Bytes
			}
			allReserved, err = add(allReserved, usage)
			if err != nil {
				return ErrInvalid
			}
			if r.UserID == candidate.UserID {
				userReserved, err = add(userReserved, usage)
				if err != nil {
					return ErrInvalid
				}
			}
			if r.RequestID == candidate.RequestID {
				requestReserved, err = add(requestReserved, usage)
				if err != nil {
					return ErrInvalid
				}
			}
		}
		if isActive(r) || !r.StagingReleased {
			allActive++
			if r.UserID == candidate.UserID {
				userActive++
			}
			if r.RequestID == candidate.RequestID {
				requestActive++
			}
		}
		if !r.StagingReleased {
			if staging > math.MaxInt64-r.StagingBytes {
				return ErrInvalid
			}
			staging += r.StagingBytes
		}
	}
	requested := Usage{Files: 1}
	if candidate.BytesKnown {
		requested.Bytes = candidate.Bytes
	}
	checks := []struct{ used, reserved, limit Usage }{{snapshot.Users[candidate.UserID], userReserved, limits.User}, {global, allReserved, limits.Instance}}
	if candidate.RequestID != "" {
		checks = append(checks, struct{ used, reserved, limit Usage }{snapshot.Requests[candidate.RequestID], requestReserved, limits.Request})
	}
	for _, check := range checks {
		ok, err := CanReserve(check.used, check.reserved, requested, check.limit)
		if err != nil {
			return ErrInvalid
		}
		if !ok {
			return ErrQuota
		}
	}
	if userActive >= limits.UserActive || allActive >= limits.InstanceActive || (candidate.RequestID != "" && requestActive >= limits.RequestActive) {
		return ErrActive
	}
	if staging > math.MaxInt64-candidate.StagingBytes {
		return ErrInvalid
	}
	if staging+candidate.StagingBytes > limits.StagingBytes {
		return ErrStaging
	}
	return nil
}
