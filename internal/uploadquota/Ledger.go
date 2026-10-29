package uploadquota

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"sort"

	"github.com/Activer007/SupaShare/internal/models"
)

func Encode(ledger Ledger) ([]byte, error) {
	if err := validateLedger(ledger); err != nil {
		return nil, err
	}
	data, err := json.Marshal(ledger)
	if err != nil || len(data) > MaxLedgerBytes {
		return nil, ErrInvalid
	}
	return data, nil
}

func Decode(data []byte) (Ledger, error) {
	if len(data) == 0 || len(data) > MaxLedgerBytes {
		return Ledger{}, ErrInvalid
	}
	check := json.NewDecoder(bytes.NewReader(data))
	check.UseNumber()
	if err := uniqueJSON(check, 0); err != nil {
		return Ledger{}, ErrInvalid
	}
	if _, err := check.Token(); err != io.EOF {
		return Ledger{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var ledger Ledger
	if err := decoder.Decode(&ledger); err != nil {
		return Ledger{}, ErrInvalid
	}
	if err := validateLedger(ledger); err != nil {
		return Ledger{}, err
	}
	return ledger, nil
}

// The standard decoder otherwise silently accepts duplicate object members.
func uniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' {
		return ErrInvalid
	}
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return ErrInvalid
		}
		if depth == 0 && name != "Version" && name != "Records" {
			return ErrInvalid
		}
		if depth == 2 && !recordFields[name] {
			return ErrInvalid
		}
		seen[name] = true
		if err := uniqueJSON(decoder, depth+1); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return ErrInvalid
	}
	return nil
}

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

var recordFields = map[string]bool{
	"ID": true, "UserID": true, "RequestID": true, "BytesKnown": true, "InitiallyUnbound": true,
	"Bytes": true, "StagingBytes": true, "StagingReleased": true,
	"CreatedAt": true, "ExpiresAt": true, "FinishedAt": true, "State": true,
	"Completing": true, "FileID": true, "CommitDigest": true,
}

func validID(id string) bool {
	return len(id) > 0 && len(id) <= MaxIDLength && idPattern.MatchString(id)
}
func validIdentity(r Reservation) bool {
	return validID(r.ID) && r.UserID > 0 && (r.RequestID == "" || validID(r.RequestID))
}
func validDeclaration(r Reservation) bool {
	return r.Bytes >= 0 && r.StagingBytes >= r.Bytes && (r.BytesKnown || (r.Bytes == 0 && r.StagingBytes == 0))
}
func isActive(r Reservation) bool { return r.State == Reserved || r.State == Uploading }

func validateLedger(ledger Ledger) error {
	if ledger.Version != LedgerVersion || ledger.Records == nil || len(ledger.Records) > MaxRecords {
		return ErrInvalid
	}
	fileIDs := make(map[string]bool)
	for id, r := range ledger.Records {
		if id != r.ID || !validIdentity(r) || !validDeclaration(r) || r.CreatedAt <= 0 || r.ExpiresAt <= r.CreatedAt || r.ExpiresAt-r.CreatedAt > MaxLifetime {
			return ErrInvalid
		}
		if !r.BytesKnown && !r.InitiallyUnbound {
			return ErrInvalid
		}
		if r.FileID != "" && !validID(r.FileID) {
			return ErrInvalid
		}
		if r.FileID != "" {
			if fileIDs[r.FileID] {
				return ErrInvalid
			}
			fileIDs[r.FileID] = true
		}
		if isActive(r) {
			if r.StagingReleased || r.FinishedAt != 0 || r.CommitDigest != "" || (r.State == Uploading && !r.BytesKnown) || (r.Completing != (r.FileID != "")) || (r.State == Reserved && r.Completing) {
				return ErrInvalid
			}
		} else {
			if r.Completing || r.FinishedAt < r.CreatedAt {
				return ErrInvalid
			}
			switch r.State {
			case Committed:
				if !r.BytesKnown || r.FileID == "" || len(r.CommitDigest) != sha256.Size*2 || r.FinishedAt >= r.ExpiresAt {
					return ErrInvalid
				}
				if _, err := hex.DecodeString(r.CommitDigest); err != nil {
					return ErrInvalid
				}
			case Aborted, Expired:
				if !r.StagingReleased || r.CommitDigest != "" || (r.State == Expired && r.FinishedAt < r.ExpiresAt) {
					return ErrInvalid
				}
			default:
				return ErrInvalid
			}
		}
	}
	return nil
}

func metadataDigest(file models.File) (string, error) {
	data, err := json.Marshal(file)
	if err != nil {
		return "", ErrInvalid
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// MatchesCommittedFile is deliberately strict for startup recovery. Later
// metadata changes require manual review instead of guessing the old commit.
func (r Reservation) MatchesCommittedFile(file models.File) bool {
	if r.State != Committed || r.FileID == "" || file.Id != r.FileID || file.UserId != r.UserID || file.UploadRequestId != r.RequestID || file.SizeBytes != r.Bytes {
		return false
	}
	digest, err := metadataDigest(file)
	return err == nil && digest == r.CommitDigest
}

// RecoveryBatch enumerates active or cleanup-pending records in stable ID order.
// It does not release resources or perform physical cleanup.
func (ledger Ledger) RecoveryBatch(after string, limit int) ([]Reservation, string, error) {
	if err := validateLedger(ledger); err != nil {
		return nil, "", err
	}
	if limit < 1 || limit > 128 || (after != "" && !validID(after)) {
		return nil, "", ErrInvalid
	}
	ids := make([]string, 0, len(ledger.Records))
	for id, r := range ledger.Records {
		if id > after && (isActive(r) || !r.StagingReleased) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	next := ""
	if len(ids) > limit {
		ids = ids[:limit]
		next = ids[len(ids)-1]
	}
	result := make([]Reservation, 0, len(ids))
	for _, id := range ids {
		result = append(result, ledger.Records[id])
	}
	return result, next, nil
}
