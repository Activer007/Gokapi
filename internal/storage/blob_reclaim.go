package storage

import (
	"context"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/blobreclaim"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// OldContentCleanupResult counts retired locator candidates, independently of
// share metadata and its logical quota. No share identity or locator is exposed.
type OldContentCleanupResult struct {
	Retired int
	Deleted int
	Shared  int
	Missing int
}

// A small pass leaves time for ordinary share cleanup. Durable candidates are
// never truncated or dropped: the cursor only chooses the next checked attempt.
const oldContentBatchSize = 128
const oldContentBatchTimeout = 10 * time.Second

var oldContentCursor struct {
	sync.Mutex
	key string
}

func reclaimOldContentContext(ctx context.Context, dataDir string) (OldContentCleanupResult, error) {
	var result OldContentCleanupResult
	if ctx == nil {
		return result, uploadquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, oldContentBatchTimeout)
	defer cancel()
	candidates, err := database.ReadBlobCandidates(ctx)
	if err != nil {
		return result, err
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Key() < candidates[j].Key() })
	oldContentCursor.Lock()
	last := oldContentCursor.key
	oldContentCursor.Unlock()
	start := sort.Search(len(candidates), func(i int) bool { return candidates[i].Key() > last })
	var firstErr error
	for index := 0; index < len(candidates) && index < oldContentBatchSize; index++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		candidate := candidates[(start+index)%len(candidates)]
		itemCtx, cancel := context.WithTimeout(ctx, cleanupItemTimeout)
		retired, err := reclaimBlobCandidateContext(itemCtx, candidate, dataDir)
		cancel()
		oldContentCursor.Lock()
		oldContentCursor.key = candidate.Key()
		oldContentCursor.Unlock()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result.Retired += retired.Retired
		result.Deleted += retired.Deleted
		result.Shared += retired.Shared
		result.Missing += retired.Missing
	}
	return result, firstErr
}

// Lock order is blob -> database only. Never take metadata gates here: the
// candidate is not a share, and replacement takes metadata -> source blob.
func reclaimBlobCandidateContext(ctx context.Context, scanned blobreclaim.Candidate, dataDir string) (OldContentCleanupResult, error) {
	var result OldContentCleanupResult
	if ctx == nil {
		return result, uploadquota.ErrInvalid
	}
	if err := scanned.Validate(); err != nil {
		return result, err
	}
	locator := models.File{SHA1: scanned.SHA1, AwsBucket: scanned.AwsBucket}
	unlock, err := lockBlobPublication(ctx, locator)
	if err != nil {
		return result, err
	}
	defer unlock()
	current, found, err := database.ReadBlobCandidate(ctx, scanned.Key())
	if err != nil || !found {
		return result, err
	}
	if current != scanned {
		return result, uploadquota.ErrConflict
	}
	// Claim does not persist a locator. An interrupted/unknown publication may
	// therefore own this blob even when no metadata is visible. Preserve the M3
	// recovery boundary instead of guessing its scope from ID, age or filename.
	// Fresh publishers of this locator cannot pass our blob gate after this read.
	if err := checkCanonicalDeletionPublication(ctx); err != nil {
		return result, err
	}
	// Includes the former target, other owners, expired and pending shares. A
	// failed/incomplete scan cannot authorize physical I/O or candidate retirement.
	shared, err := database.HasAnyBlobReference(ctx, current)
	if err != nil {
		return result, err
	}
	deleted, missing := false, false
	if !shared {
		var local *os.Root
		if locator.IsLocalStorage() {
			if dataDir == "" {
				return result, uploadquota.ErrInvalid
			}
			local, err = os.OpenRoot(dataDir)
			if err != nil {
				return result, err
			}
			defer local.Close()
		}
		exists, err := probeCleanupSource(ctx, locator, local)
		if err != nil {
			return result, err
		}
		if exists {
			if err := deleteSource(ctx, locator, local); err != nil {
				return result, err
			}
			exists, err = probeCleanupSource(ctx, locator, local)
			if err != nil {
				return result, err
			}
			if exists {
				return result, errCleanupSourcePresent
			}
			deleted = true
		} else {
			missing = true
		}
	}
	// A surviving metadata reference becomes the retry anchor. Its later
	// replacement atomically registers a fresh token; this exact ack cannot
	// discard that newer intent. Candidate retirement never releases quota.
	retired, err := database.RetireBlobCandidate(ctx, current)
	if err != nil || !retired {
		return result, err
	}
	result.Retired = 1
	switch {
	case shared:
		result.Shared = 1
	case deleted:
		result.Deleted = 1
	case missing:
		result.Missing = 1
	}
	return result, nil
}

// Both canonical cleanup paths must preserve ambiguous publications. Checking
// only detached candidates would let an expired metadata row delete the same
// source later in the very same GC pass. The caller holds the locator's gate.
func checkCanonicalDeletionPublication(ctx context.Context) error {
	ledger, err := database.ReadUploadQuota(ctx)
	if err != nil {
		return err
	}
	for _, reservation := range ledger.Records {
		if reservation.Completing || (reservation.State == uploadquota.Committed && !reservation.StagingReleased) {
			return uploadquota.ErrInProgress
		}
	}
	return nil
}
