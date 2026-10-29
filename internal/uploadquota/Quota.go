// Package uploadquota supplies pure accounting and reservation transitions.
// Providers persist them atomically; HTTP upload admission is a separate layer.
package uploadquota

import (
	"errors"
	"math"

	"github.com/Activer007/SupaShare/internal/models"
)

// Usage measures logical bytes and file records, without deduplication.
type Usage struct {
	Bytes int64
	Files int64
}

// Snapshot groups metadata usage by current owner and original File Request.
type Snapshot struct {
	Users    map[int]Usage
	Requests map[string]Usage
}

// Measure counts every supplied metadata record, including expired and pending
// deletion records. SizeBytes is the stored upload length, including ciphertext
// for E2E uploads; display sizes and shared content hashes do not reduce usage.
// Ownership transfer changes the user charged but retains the request charge.
// Keys must match each nonempty metadata ID, as returned by GetAllMetadata.
// Invalid input or arithmetic overflow returns no partial snapshot.
func Measure(files map[string]models.File) (Snapshot, error) {
	snapshot := Snapshot{Users: make(map[int]Usage), Requests: make(map[string]Usage)}
	for id, file := range files {
		if id == "" || file.Id != id || file.UserId <= 0 || file.SizeBytes < 0 {
			return Snapshot{}, errors.New("invalid file metadata for quota measurement")
		}
		usage := Usage{Bytes: file.SizeBytes, Files: 1}
		userUsage, err := add(snapshot.Users[file.UserId], usage)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.Users[file.UserId] = userUsage
		if file.UploadRequestId != "" {
			requestUsage, err := add(snapshot.Requests[file.UploadRequestId], usage)
			if err != nil {
				return Snapshot{}, err
			}
			snapshot.Requests[file.UploadRequestId] = requestUsage
		}
	}
	return snapshot, nil
}

// CanReserve compares used + reserved + requested to literal capacity limits.
// Zero means no capacity, rather than unlimited capacity.
// It only calculates a decision; callers must enforce it atomically with their
// reservation write. All values must be nonnegative and additions must fit int64.
func CanReserve(used, reserved, requested, limit Usage) (bool, error) {
	for _, usage := range []Usage{used, reserved, requested, limit} {
		if usage.Bytes < 0 || usage.Files < 0 {
			return false, errors.New("quota usage and limits must be nonnegative")
		}
	}
	total, err := add(used, reserved)
	if err != nil {
		return false, err
	}
	total, err = add(total, requested)
	if err != nil {
		return false, err
	}
	return total.Bytes <= limit.Bytes && total.Files <= limit.Files, nil
}

// add receives only nonnegative usage from the validated public entry points.
func add(a, b Usage) (Usage, error) {
	if a.Bytes > math.MaxInt64-b.Bytes || a.Files > math.MaxInt64-b.Files {
		return Usage{}, errors.New("quota usage exceeds int64 capacity")
	}
	return Usage{Bytes: a.Bytes + b.Bytes, Files: a.Files + b.Files}, nil
}
