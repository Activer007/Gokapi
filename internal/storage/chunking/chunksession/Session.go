// Package chunksession owns durable, scoped chunk upload progress. It assumes a
// single process and a protected DataDir; quota transactions never span file I/O.
package chunksession

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/environment"
	"github.com/Activer007/Gokapi/internal/helper"
	"github.com/Activer007/Gokapi/internal/storage/chunking"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

type Scope struct {
	ClientID  string
	UserID    int
	RequestID string
}

// ID leaves legacy client identifiers unchanged on the wire, while the ledger
// identity is the existing authenticated storage identity in a disjoint domain.
func ID(scope Scope) string {
	return "chunk." + chunking.StorageID(scope.ClientID, scope.UserID, scope.RequestID)
}

type Store struct {
	root        string
	apply       func(context.Context, uploadquota.Change) (uploadquota.Reservation, error)
	read        func(context.Context) (uploadquota.Ledger, error)
	now         func() int64
	closeFile   func(*os.File) error
	freeSpace   func(string) (uint64, error)
	freeFloor   func() (int64, error)
	mu          sync.Mutex
	initialized bool
	gates       map[string]*gate
	uncertain   map[string]error
}

// New takes the configured DataDir. One Store must be reused for that DataDir
// throughout the process; sharing it with another process is unsupported.
func New(root string) *Store {
	return &Store{root: root, apply: database.ApplyUploadQuota, read: database.ReadUploadQuota,
		now: func() int64 { return time.Now().Unix() }, closeFile: func(f *os.File) error { return f.Close() },
		freeSpace: helper.GetFreeSpace, freeFloor: func() (int64, error) {
			mib := environment.New().MinFreeSpaceMB
			if mib < 0 || int64(mib) > math.MaxInt64/(1<<20) {
				return 0, uploadquota.ErrInvalid
			}
			return int64(mib) * (1 << 20), nil
		},
		gates: make(map[string]*gate), uncertain: make(map[string]error)}
}

func (s *Store) Reserve(ctx context.Context, scope Scope, limits uploadquota.Limits) (uploadquota.Reservation, error) {
	if err := validScope(ctx, scope); err != nil {
		return uploadquota.Reservation{}, err
	}
	if scope.RequestID == "" {
		return uploadquota.Reservation{}, uploadquota.ErrInvalid
	}
	release, err := s.acquire(ctx, ID(scope), false)
	if err != nil {
		return uploadquota.Reservation{}, err
	}
	defer release()
	if err := s.prepareRoot(ctx); err != nil {
		return uploadquota.Reservation{}, err
	}
	now := s.now()
	return s.apply(ctx, uploadquota.Change{Action: uploadquota.Reserve, Upload: uploadquota.Reservation{ID: ID(scope), UserID: scope.UserID, RequestID: scope.RequestID, ExpiresAt: now + uploadquota.MaxLifetime}, Now: now, Limits: limits, CheckRequestPolicy: true})
}

func (s *Store) Write(ctx context.Context, scope Scope, info chunking.ChunkInfo, input io.Reader, payloadBytes, maxFileBytes int64, limits uploadquota.Limits) (uploadquota.Reservation, error) {
	if err := validScope(ctx, scope); err != nil {
		return uploadquota.Reservation{}, err
	}
	total := info.TotalFilesizeBytes
	if input == nil || maxFileBytes <= 0 || total < 0 || total > maxFileBytes || total > (math.MaxInt64-stagingOverhead)/4 || payloadBytes < 0 || info.Offset < 0 || info.Offset > total || payloadBytes > total-info.Offset || (total > 0 && payloadBytes == 0) || (info.UUID != "" && info.UUID != scope.ClientID) {
		return uploadquota.Reservation{}, uploadquota.ErrInvalid
	}
	id := ID(scope)
	release, err := s.acquire(ctx, id, false)
	if err != nil {
		return uploadquota.Reservation{}, err
	}
	defer release()
	if err := s.prepareRoot(ctx); err != nil {
		return uploadquota.Reservation{}, err
	}
	r, found, err := s.record(ctx, scope)
	if err != nil {
		return r, err
	}
	declaration := uploadquota.Reservation{ID: id, UserID: scope.UserID, RequestID: scope.RequestID, BytesKnown: true, Bytes: total, StagingBytes: 4*total + stagingOverhead, ExpiresAt: s.now() + uploadquota.MaxLifetime}
	if !found {
		// Public request IDs must already have been generated and admitted by
		// Reserve. Ordinary client IDs are admitted implicitly on first write.
		if scope.RequestID != "" {
			return r, uploadquota.ErrNotFound
		}
		r, err = s.apply(ctx, uploadquota.Change{Action: uploadquota.Reserve, Upload: declaration, Limits: limits, Now: s.now()})
		if err != nil {
			return r, err
		}
	}
	if err := writable(r, s.now()); err != nil {
		return r, err
	}
	if r.BytesKnown && (r.Bytes != total || r.StagingBytes != declaration.StagingBytes) {
		return r, uploadquota.ErrConflict
	}
	// Recheck current request policy and aggregate limits even on a retry.
	r, err = s.apply(ctx, uploadquota.Change{Action: uploadquota.Bind, Upload: declaration, Limits: limits, Now: s.now(), CheckRequestPolicy: scope.RequestID != ""})
	if err != nil {
		return r, err
	}
	m, err := s.loadProgress(id, r, true)
	if err != nil {
		return r, err
	}
	end := info.Offset + payloadBytes // subtraction bounds above make this safe.
	duplicate := -1
	for i, part := range m.Ranges {
		if part.Offset == info.Offset && part.Length == payloadBytes {
			duplicate = i
			break
		}
		if info.Offset < part.Offset+part.Length && part.Offset < end {
			return r, uploadquota.ErrConflict
		}
	}
	if duplicate < 0 && len(m.Ranges) >= maxRanges {
		return r, uploadquota.ErrFull
	}
	if duplicate >= 0 {
		digest, err := copyExact(ctx, io.Discard, input, payloadBytes)
		if err != nil {
			return r, err
		}
		if digest != m.Ranges[duplicate].SHA256 {
			return r, uploadquota.ErrConflict
		}
		return r, nil
	}
	file, err := s.openPayload(id, r.Bytes, len(m.Ranges) == 0)
	if err != nil {
		return r, err
	}
	digest, writeErr := copyExact(ctx, io.NewOffsetWriter(file, info.Offset), input, payloadBytes)
	if writeErr == nil {
		writeErr = ctx.Err()
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := s.closeOwned(id, file)
	if writeErr != nil || closeErr != nil {
		return r, errors.Join(writeErr, closeErr)
	}
	m.Ranges = append(m.Ranges, partReceipt{Offset: info.Offset, Length: payloadBytes, SHA256: digest})
	sort.Slice(m.Ranges, func(i, j int) bool { return m.Ranges[i].Offset < m.Ranges[j].Offset })
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if err := s.saveProgress(id, m); err != nil {
		return r, err
	}
	return r, nil
}

func (s *Store) Prepare(ctx context.Context, scope Scope, header chunking.FileHeader, newFileID string, limits uploadquota.Limits) (*Lease, error) {
	if err := validScope(ctx, scope); err != nil {
		return nil, err
	}
	id := ID(scope)
	release, err := s.acquire(ctx, id, true)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			release()
		}
	}()
	if err := s.prepareRoot(ctx); err != nil {
		return nil, err
	}
	r, found, err := s.record(ctx, scope)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, uploadquota.ErrNotFound
	}
	if !r.BytesKnown || header.Size != r.Bytes {
		return nil, uploadquota.ErrConflict
	}
	if r.State == uploadquota.Committed {
		s.completing(id)
		keep = true
		return &Lease{Upload: r, AlreadyCommitted: true, store: s, release: release, id: id, receiptOnly: true}, nil
	}
	if err := writable(r, s.now()); err != nil {
		return nil, err
	}
	m, err := s.loadProgress(id, r, false)
	if err != nil {
		return nil, err
	}
	if !m.complete() {
		return nil, errors.Join(uploadquota.ErrConflict, errors.New("chunk coverage is incomplete"))
	}
	file, err := s.openPayload(id, r.Bytes, false)
	if err != nil {
		return nil, err
	}
	// The read descriptor is retained by the lease. No I/O transaction is held
	// across Claim or across preparation/publication in the storage layer.
	r, err = s.apply(ctx, uploadquota.Change{Action: uploadquota.Claim, Upload: r, FileID: newFileID, Limits: limits, Now: s.now(), CheckRequestPolicy: scope.RequestID != ""})
	if err != nil {
		return nil, errors.Join(err, s.closeOwned(id, file))
	}
	s.completing(id)
	keep = true
	return &Lease{Upload: r, Input: file, store: s, release: release, id: id, input: file}, nil
}

func (s *Store) Cancel(ctx context.Context, scope Scope) error {
	if err := validScope(ctx, scope); err != nil {
		return err
	}
	// Unknown, foreign-scope and committed IDs retain the legacy unreserve
	// no-op semantics. Inspect scope before a local gate can expose another
	// request's in-progress state. This never authorizes cleanup or a refund.
	r, found, err := s.cancelRecord(ctx, scope)
	if err != nil {
		return err
	}
	if !found || r.State == uploadquota.Committed {
		return nil
	}
	release, err := s.acquire(ctx, ID(scope), false)
	if err != nil {
		if errors.Is(err, uploadquota.ErrInProgress) {
			// Commit may have finished between the first read and the claim
			// signal. A fresh receipt still permits only a no-op, not cleanup.
			current, found, readErr := s.cancelRecord(ctx, scope)
			if readErr != nil {
				return errors.Join(err, readErr)
			}
			if !found || current.State == uploadquota.Committed {
				return nil
			}
		}
		return err
	}
	defer release()
	if err := s.prepareRoot(ctx); err != nil {
		return err
	}
	r, found, err = s.cancelRecord(ctx, scope)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if r.State == uploadquota.Committed {
		return nil
	}
	if r.Completing {
		return uploadquota.ErrInProgress
	}
	if r.State == uploadquota.Aborted || r.State == uploadquota.Expired {
		return nil
	}
	return s.abort(ctx, r, uploadquota.Aborted)
}

func (s *Store) cancelRecord(ctx context.Context, scope Scope) (uploadquota.Reservation, bool, error) {
	ledger, err := s.read(ctx)
	if err != nil {
		return uploadquota.Reservation{}, false, err
	}
	r, found := ledger.Records[ID(scope)]
	if !found || r.UserID != scope.UserID || r.RequestID != scope.RequestID {
		return uploadquota.Reservation{}, false, nil
	}
	return r, true, nil
}

// Sweep only cleans unclaimed expired chunk sessions. Missing payload is never
// evidence that an uncertain publication or Commit has been cleaned up.
func (s *Store) Sweep(ctx context.Context) error {
	if ctx == nil {
		return uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.prepareRoot(ctx); err != nil {
		return err
	}
	ledger, err := s.read(ctx)
	if err != nil {
		return err
	}
	var result error
	for id, r := range ledger.Records {
		if !strings.HasPrefix(id, "chunk.") || r.Completing || r.ExpiresAt > s.now() || (r.State != uploadquota.Reserved && r.State != uploadquota.Uploading) {
			continue
		}
		release, err := s.acquire(ctx, id, false)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		current, readErr := s.read(ctx)
		if readErr != nil {
			result = errors.Join(result, readErr)
		} else if r, ok := current.Records[id]; ok && !r.Completing && r.ExpiresAt <= s.now() && (r.State == uploadquota.Reserved || r.State == uploadquota.Uploading) {
			result = errors.Join(result, s.abort(ctx, r, uploadquota.Expired))
		}
		release()
		if ctx.Err() != nil {
			return errors.Join(result, ctx.Err())
		}
	}
	return result
}

func (s *Store) record(ctx context.Context, scope Scope) (uploadquota.Reservation, bool, error) {
	ledger, err := s.read(ctx)
	if err != nil {
		return uploadquota.Reservation{}, false, err
	}
	r, ok := ledger.Records[ID(scope)]
	if ok && (r.UserID != scope.UserID || r.RequestID != scope.RequestID) {
		return uploadquota.Reservation{}, false, uploadquota.ErrConflict
	}
	return r, ok, nil
}

func (s *Store) abort(ctx context.Context, r uploadquota.Reservation, terminal uploadquota.State) error {
	if err := s.cleanup(r.ID); err != nil {
		return err
	}
	_, err := s.apply(ctx, uploadquota.Change{Action: uploadquota.Abort, Upload: r, Now: s.now(), Terminal: terminal, CleanupConfirmed: true, CheckRequestPolicy: r.RequestID != ""})
	return err
}

func writable(r uploadquota.Reservation, now int64) error {
	if r.Completing {
		return uploadquota.ErrInProgress
	}
	if r.State != uploadquota.Reserved && r.State != uploadquota.Uploading {
		return uploadquota.ErrConflict
	}
	if r.ExpiresAt <= now {
		return uploadquota.ErrExpired
	}
	return nil
}

type Lease struct {
	Upload           uploadquota.Reservation
	Input            *os.File
	AlreadyCommitted bool
	store            *Store
	release          func()
	mu               sync.Mutex
	released         bool
	closed           bool
	closeErr         error
	id               string
	input            *os.File
	receiptOnly      bool
}

// CloseInput confirms only this session's input cleanup, never storage work or
// a quota refund. A failed close remains sticky until the process exits.
func (l *Lease) CloseInput() error {
	if l == nil {
		return uploadquota.ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.closeErr
	}
	if l.receiptOnly {
		l.closed = true
		return nil
	}
	if l.released || l.input == nil {
		return uploadquota.ErrInvalid
	}
	l.closed = true
	l.closeErr = l.store.closeOwned(l.id, l.input)
	if l.closeErr == nil {
		l.closeErr = l.store.cleanup(l.id)
	}
	return l.closeErr
}

// Release releases the gate, not the staging charge. Call CloseInput first when
// cleanup is known to be appropriate; a recovered claimed upload stays held.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.released {
		l.released = true
		l.release()
	}
}
