package chunkreservation

import (
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/helper"
)

var reservedChunks = make(map[string]map[string]reservation)
var reservationMutex sync.RWMutex
var runGcOnce = sync.Once{}

const timeReservationWithoutUpload = 4 * 60
const timeReservationWithUpload = 23 * 60 * 60

type reservation struct {
	Uuid   string
	Expiry int64
}

// GetCount returns the number of chunks reserved for the given file request
func GetCount(id string) int {
	reservationMutex.RLock()
	defer reservationMutex.RUnlock()
	length := len(reservedChunks[id])
	return length
}

// New creates a new chunk reservation
func New(id string) string {
	reservationMutex.Lock()
	defer reservationMutex.Unlock()

	uuid := helper.GenerateRandomString(32)
	if reservedChunks[id] == nil {
		reservedChunks[id] = make(map[string]reservation)
	}
	reservedChunks[id][uuid] = reservation{
		Uuid:   uuid,
		Expiry: time.Now().Unix() + timeReservationWithoutUpload,
	}

	runGcOnce.Do(func() { go cleanUp(true) })
	return uuid
}

// SetComplete marks a chunk as complete or cancelled
func SetComplete(id, uuid string) {
	reservationMutex.Lock()
	delete(reservedChunks[id], uuid)
	reservationMutex.Unlock()
}

// Cancel removes only a live reservation belonging to this request. Keep the
// check, file removal and reservation removal together so duplicate cancels
// cannot invoke remove twice. Failed removal retains the reservation for retry.
// Unknown reservations are a no-op, preserving idempotent cancellation.
func Cancel(id, uuid string, remove func() error) error {
	reservationMutex.Lock()
	defer reservationMutex.Unlock()
	chunk, ok := reservedChunks[id][uuid]
	if !ok || chunk.Expiry <= time.Now().Unix() {
		return nil
	}
	if err := remove(); err != nil {
		return err
	}
	delete(reservedChunks[id], uuid)
	return nil
}

// IsValid reports whether the request still owns a live reservation. It does
// not extend the lifetime or consume the reservation.
func IsValid(id, uuid string) bool {
	reservationMutex.RLock()
	defer reservationMutex.RUnlock()
	chunk, ok := reservedChunks[id][uuid]
	return ok && chunk.Expiry > time.Now().Unix()
}

// SetUploading marks a chunk as uploading and extends the reservation time
func SetUploading(id string, uuid string) bool {
	reservationMutex.Lock()
	defer reservationMutex.Unlock()

	if reservedChunks[id] == nil {
		return false
	}
	chunk, ok := reservedChunks[id][uuid]
	if !ok {
		return false
	}
	if chunk.Expiry <= time.Now().Unix() {
		return false
	}
	chunk.Expiry = time.Now().Unix() + timeReservationWithUpload
	reservedChunks[id][uuid] = chunk
	return true
}

func cleanUp(isPeriodic bool) {
	reservationMutex.Lock()
	for id, chunks := range reservedChunks {
		now := time.Now().Unix()
		for uuid, reservedChunk := range chunks {
			if reservedChunk.Expiry < now {
				delete(reservedChunks[id], uuid)
			}
		}
	}
	reservationMutex.Unlock()

	if isPeriodic {
		go func() {
			time.Sleep(time.Minute * 5)
			cleanUp(true)
		}()
	}
}
