package chunksession

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Activer007/Gokapi/internal/uploadquota"
)

// VerifyRecovery checks existing private inputs before startup cleanup begins.
// It never creates a namespace, removes progress, or changes quota state.
func (s *Store) VerifyRecovery(ctx context.Context) error {
	if ctx == nil {
		return uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	abs, err := filepath.Abs(s.root)
	if err != nil {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	s.root = abs
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	if _, err := os.Lstat(s.path("")); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return errors.Join(uploadquota.ErrInvalid, err)
	}
	_, err = s.verifyEntriesLocked(ctx)
	return err
}

// CleanupCommittedInput removes only a confirmed receipt's owned private input.
// The coordinator must separately confirm all staging cleanup before refunding;
// this method never releases quota, recreates metadata, or touches canonical.
func (s *Store) CleanupCommittedInput(ctx context.Context, expected uploadquota.Reservation) error {
	if ctx == nil || !strings.HasPrefix(expected.ID, "chunk.") || len(expected.ID) > uploadquota.MaxIDLength || expected.UserID <= 0 {
		return uploadquota.ErrInvalid
	}
	release, err := s.acquire(ctx, expected.ID, true)
	if err != nil {
		return err
	}
	defer release()
	ledger, err := s.read(ctx)
	if err != nil {
		return err
	}
	current, ok := ledger.Records[expected.ID]
	if !ok || current != expected || current.State != uploadquota.Committed || current.StagingReleased {
		return uploadquota.ErrConflict
	}
	if err := s.prepareRoot(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.cleanup(current.ID)
}
