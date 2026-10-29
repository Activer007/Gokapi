package apimutex

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
)

const numStripes = 256

type stripe struct {
	token chan struct{}
}

var stripes [numStripes]stripe
var stripesOnce sync.Once
var errNilLockContext = errors.New("nil lock context")

func initStripes() {
	stripesOnce.Do(func() {
		for i := range stripes {
			stripes[i].token = make(chan struct{}, 1)
			stripes[i].token <- struct{}{}
		}
	})
}

func getStripe(objectType int, key string) *stripe {
	index := stripeIndex(objectType, key)
	initStripes()
	return &stripes[index]
}

func (s *stripe) Lock() {
	<-s.token
}

func (s *stripe) Unlock() {
	select {
	case s.token <- struct{}{}:
	default:
		panic("unlock of unlocked stripe")
	}
}

func (s *stripe) lockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.token:
	}
	// A permit and cancellation can become ready together. Never return a
	// held permit to a caller whose context was already canceled.
	if err := ctx.Err(); err != nil {
		s.Unlock()
		return err
	}
	return nil
}

func stripeIndex(objectType int, key string) uint32 {
	switch objectType {
	case TypeUser, TypeApiKey, TypeMetaData:
		// valid
	default:
		panic("invalid object type")
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte{byte(objectType)})
	_, _ = h.Write([]byte(key))
	return h.Sum32() % numStripes
}

// LockMany deduplicates shared stripes and acquires them in a fixed order.
// Different file IDs can hash to one stripe, so locking by sorted IDs is unsafe.
func LockMany(objectType int, keys ...string) func() {
	unlock, err := LockManyContext(context.Background(), objectType, keys...)
	if err != nil {
		panic(err)
	}
	return unlock
}

// LockContext acquires the same stripe used by Lock and Unlock. The returned
// function releases it exactly once; cancellation after success does not unlock.
func LockContext(ctx context.Context, objectType int, key string) (func(), error) {
	if ctx == nil {
		return nil, errNilLockContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selected := getStripe(objectType, key)
	if err := selected.lockContext(ctx); err != nil {
		return nil, err
	}
	return selected.Unlock, nil
}

// LockManyContext uses the legacy stripe set, with deduplication and ascending
// acquisition order. Cancellation releases only acquired stripes, in reverse.
func LockManyContext(ctx context.Context, objectType int, keys ...string) (func(), error) {
	if ctx == nil {
		return nil, errNilLockContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return func() {}, nil
	}
	var selected [numStripes]bool
	for _, key := range keys {
		selected[stripeIndex(objectType, key)] = true
	}
	initStripes()
	var acquired []int
	release := func() {
		for i := len(acquired) - 1; i >= 0; i-- {
			stripes[acquired[i]].Unlock()
		}
	}
	for i := range selected {
		if selected[i] {
			if err := stripes[i].lockContext(ctx); err != nil {
				release()
				return nil, err
			}
			acquired = append(acquired, i)
		}
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

const (
	TypeUser = iota
	TypeApiKey
	TypeMetaData
)

func Lock(objectType int, key string) {
	getStripe(objectType, key).Lock()
}

func Unlock(objectType int, key string) {
	getStripe(objectType, key).Unlock()
}
