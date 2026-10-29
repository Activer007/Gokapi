package storage

import (
	"errors"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
)

const deletionTimerCapacity = 16384

var (
	errDeletionTimerCapacity = errors.New("deletion timer capacity reached")
	errDeletionTimerReserved = errors.New("deletion timer already reserved")
	errDeletionTimerInvalid  = errors.New("invalid deletion timer reservation")
	deletionTimers           *deletionTimerRegistry
)

func init() {
	// Install after package variable initialization: executeDeletionTimer itself
	// checks this registry, so using it in a variable initializer forms a cycle.
	deletionTimers = newDeletionTimerRegistry(deletionTimerCapacity, executeDeletionTimer)
}

// A nonzero-sized allocation gives every in-process intent a distinct identity.
// The token is not persisted and does not replace the callback's database check.
type deletionTimerToken struct {
	identity byte
}

type deletionTimerSpec struct {
	File         models.File
	Deadline     time.Time
	DeleteSource bool
}

type deletionTimerRegistry struct {
	mu       sync.Mutex
	capacity int
	entries  map[string]*deletionTimerEntry
	execute  func(string, *deletionTimerToken)
}

type deletionTimerEntry struct {
	id          string
	token       *deletionTimerToken
	spec        deletionTimerSpec
	hasSpec     bool
	reservation *deletionTimerReservation
	timer       *time.Timer
	// outstanding includes an armed timer and a queued or running callback.
	// Stop(false) must leave this charge until finish observes its return.
	outstanding   bool
	callbackToken *deletionTimerToken
}

type deletionTimerReservation struct {
	registry *deletionTimerRegistry
	entry    *deletionTimerEntry
	token    *deletionTimerToken
	done     bool // Protected by registry.mu.
}

func newDeletionTimerRegistry(capacity int, execute func(string, *deletionTimerToken)) *deletionTimerRegistry {
	if capacity > deletionTimerCapacity {
		capacity = deletionTimerCapacity
	}
	return &deletionTimerRegistry{
		capacity: capacity,
		entries:  make(map[string]*deletionTimerEntry),
		execute:  execute,
	}
}

// reserve is called before changing metadata. It preserves the previous intent
// until commit, so a failed database mutation can abort without losing a timer.
func (r *deletionTimerRegistry) reserve(id string) (*deletionTimerReservation, error) {
	if r == nil || id == "" || r.capacity < 1 || r.execute == nil {
		return nil, errDeletionTimerInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[id]
	if entry == nil {
		if len(r.entries) >= r.capacity {
			return nil, errDeletionTimerCapacity
		}
		entry = &deletionTimerEntry{id: id}
		r.entries[id] = entry
	}
	if entry.reservation != nil {
		return nil, errDeletionTimerReserved
	}
	reservation := &deletionTimerReservation{
		registry: r,
		entry:    entry,
		token:    &deletionTimerToken{},
	}
	entry.reservation = reservation
	return reservation, nil
}

func (reservation *deletionTimerReservation) commit(spec deletionTimerSpec) {
	if reservation == nil || reservation.registry == nil {
		return
	}
	r := reservation.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := reservation.entry
	if reservation.done || r.entries[entry.id] != entry || entry.reservation != reservation {
		return
	}
	reservation.done = true
	entry.reservation = nil
	if spec.File.Id != entry.id || spec.Deadline.IsZero() {
		r.releaseIdleLocked(entry)
		return
	}
	entry.token = reservation.token
	entry.spec = cloneDeletionTimerSpec(spec)
	entry.hasSpec = true
	r.stopLocked(entry)
	if !entry.outstanding {
		r.armLocked(entry)
	}
}

func (reservation *deletionTimerReservation) abort() {
	if reservation == nil || reservation.registry == nil {
		return
	}
	r := reservation.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := reservation.entry
	if reservation.done || r.entries[entry.id] != entry || entry.reservation != reservation {
		return
	}
	reservation.done = true
	entry.reservation = nil
	r.releaseIdleLocked(entry)
}

// invalidate never waits for execution. A callback that has already been queued
// keeps its entry until it returns, and must recheck current under the metadata
// lock before changing any file state.
func (r *deletionTimerRegistry) invalidate(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[id]
	if entry == nil {
		return
	}
	entry.token = nil
	entry.spec = deletionTimerSpec{}
	entry.hasSpec = false
	if entry.reservation != nil {
		entry.reservation.done = true
		entry.reservation = nil
	}
	r.stopLocked(entry)
	r.releaseIdleLocked(entry)
}

func (r *deletionTimerRegistry) current(id string, token *deletionTimerToken) (deletionTimerSpec, bool) {
	if r == nil || token == nil {
		return deletionTimerSpec{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.entries[id]
	if entry == nil || !entry.hasSpec || entry.token != token {
		return deletionTimerSpec{}, false
	}
	return cloneDeletionTimerSpec(entry.spec), true
}

func (r *deletionTimerRegistry) stopLocked(entry *deletionTimerEntry) {
	if entry.timer != nil && entry.timer.Stop() {
		entry.timer = nil
		entry.outstanding = false
		entry.callbackToken = nil
	}
}

func (r *deletionTimerRegistry) armLocked(entry *deletionTimerEntry) {
	token := entry.token
	entry.outstanding = true
	entry.callbackToken = token
	delay := time.Until(entry.spec.Deadline)
	if delay < 0 {
		delay = 0
	}
	entry.timer = time.AfterFunc(delay, func() {
		r.fire(entry, token)
	})
}

func (r *deletionTimerRegistry) fire(entry *deletionTimerEntry, token *deletionTimerToken) {
	r.mu.Lock()
	if r.entries[entry.id] != entry || !entry.outstanding || entry.callbackToken != token {
		r.mu.Unlock()
		return
	}
	entry.timer = nil
	execute := entry.hasSpec && entry.token == token
	r.mu.Unlock()
	defer r.finish(entry, token)
	if execute {
		r.execute(entry.id, token)
	}
}

func (r *deletionTimerRegistry) finish(entry *deletionTimerEntry, token *deletionTimerToken) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries[entry.id] != entry || entry.callbackToken != token {
		return
	}
	entry.timer = nil
	entry.outstanding = false
	entry.callbackToken = nil
	if entry.hasSpec && entry.token == token {
		// A callback is attempted once. Failure leaves persisted pending state
		// for checked periodic cleanup rather than starting a retry loop here.
		entry.token = nil
		entry.spec = deletionTimerSpec{}
		entry.hasSpec = false
	}
	if entry.hasSpec {
		r.armLocked(entry)
	} else {
		r.releaseIdleLocked(entry)
	}
}

func (r *deletionTimerRegistry) releaseIdleLocked(entry *deletionTimerEntry) {
	if !entry.outstanding && !entry.hasSpec && entry.reservation == nil {
		delete(r.entries, entry.id)
	}
}

func cloneDeletionTimerSpec(spec deletionTimerSpec) deletionTimerSpec {
	spec.File.InternalRedisEncryption = append([]byte(nil), spec.File.InternalRedisEncryption...)
	spec.File.Encryption.DecryptionKey = append([]byte(nil), spec.File.Encryption.DecryptionKey...)
	spec.File.Encryption.Nonce = append([]byte(nil), spec.File.Encryption.Nonce...)
	return spec
}
