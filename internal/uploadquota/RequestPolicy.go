package uploadquota

import "math"

// RequestPolicy is the bounded numeric projection of a current File Request.
// It contains no public capability, display text or persistent quota counters.
type RequestPolicy struct {
	UserID    int64
	MaxFiles  int64
	MaxSizeMB int64
	Expiry    int64
}

func (change Change) RequiresRequestPolicy() bool {
	if !change.CheckRequestPolicy {
		return false
	}
	switch change.Action {
	case Reserve, Bind, Claim, Commit, DuplicateMetadata:
		return true
	default:
		return false
	}
}

// ApplyRequestPolicy resolves current request limits before the atomic ledger
// transition. Providers supply the policy through the same transaction/CAS.
func ApplyRequestPolicy(ledger Ledger, change Change, policy RequestPolicy) (Change, error) {
	if !change.RequiresRequestPolicy() {
		return change, nil
	}
	if !validIdentity(change.Upload) || change.Upload.RequestID == "" || change.Now <= 0 ||
		policy.UserID <= 0 || policy.MaxFiles < 0 || policy.MaxSizeMB < 0 || policy.Expiry < 0 ||
		policy.MaxSizeMB > math.MaxInt64/(1<<20) {
		return Change{}, ErrInvalid
	}
	if change.Action == DuplicateMetadata {
		// This is an authenticated management copy of existing content, not a
		// new public upload. Preserve cross-owner copies and expired-request
		// management while retaining the original request's logical capacity.
		// Copies and ownership transfers retain the original request charge;
		// the authorized current source may therefore have a different owner.
		if change.Source == nil || change.Source.UserId <= 0 || change.Source.UploadRequestId != change.Upload.RequestID {
			return Change{}, ErrConflict
		}
		return withRequestFileLimit(change, policy), nil
	}
	if policy.UserID != int64(change.Upload.UserID) {
		return Change{}, ErrConflict
	}
	// Preserve the existing File Request Unix-second boundary. The ledger's
	// own absolute reservation expiry is checked separately by Apply.
	if policy.Expiry != 0 && policy.Expiry < change.Now {
		return Change{}, ErrExpired
	}
	bound := change.Upload
	if current, ok := ledger.Records[change.Upload.ID]; ok {
		if current.UserID != change.Upload.UserID || current.RequestID != change.Upload.RequestID {
			return Change{}, ErrConflict
		}
		if change.Action != Bind {
			bound = current
		}
	} else if change.Action != Reserve {
		return Change{}, ErrNotFound
	}
	if bound.BytesKnown && policy.MaxSizeMB != 0 && bound.Bytes > policy.MaxSizeMB*(1<<20) {
		return Change{}, ErrQuota
	}
	// Old zero cancels only the original request-specific count layer. It
	// still resolves to the finite enclosing instance capacity, never infinity.
	return withRequestFileLimit(change, policy), nil
}

func withRequestFileLimit(change Change, policy RequestPolicy) Change {
	change.Limits.Request.Files = change.Limits.Instance.Files
	if policy.MaxFiles != 0 {
		change.Limits.Request.Files = min(policy.MaxFiles, change.Limits.Instance.Files)
	}
	return change
}
