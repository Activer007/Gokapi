package apimutex

import (
	"hash/fnv"
	"sync"
)

const numStripes = 256

var stripes [numStripes]sync.Mutex

func getStripe(objectType int, key string) *sync.Mutex {
	return &stripes[stripeIndex(objectType, key)]
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
	var selected [numStripes]bool
	for _, key := range keys {
		selected[stripeIndex(objectType, key)] = true
	}
	for i := range selected {
		if selected[i] {
			stripes[i].Lock()
		}
	}
	return func() {
		for i := len(selected) - 1; i >= 0; i-- {
			if selected[i] {
				stripes[i].Unlock()
			}
		}
	}
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
