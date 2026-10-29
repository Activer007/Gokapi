//go:build test && security_replace

package apimutex

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSEC030ReplaceLocks(t *testing.T) {
	left, collision := "replace-left", ""
	for i := 0; i < 4096; i++ {
		key := fmt.Sprintf("replace-collision-%d", i)
		if stripeIndex(TypeMetaData, key) == stripeIndex(TypeMetaData, left) {
			collision = key
			break
		}
	}
	if collision == "" {
		t.Fatal("no deterministic collision fixture found")
	}
	done := make(chan struct{})
	go func() {
		unlock := LockMany(TypeMetaData, left, left, collision)
		unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("duplicate IDs or colliding stripes self-deadlocked")
	}
	var workers sync.WaitGroup
	count := 0
	start := make(chan struct{})
	for i := 0; i < 50; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			keys := []string{left, "replace-right"}
			if i%2 != 0 {
				keys[0], keys[1] = keys[1], keys[0]
			}
			<-start
			unlock := LockMany(TypeMetaData, keys...)
			count++
			unlock()
		}(i)
	}
	close(start)
	complete := make(chan struct{})
	go func() { workers.Wait(); close(complete) }()
	select {
	case <-complete:
		if count != 50 {
			t.Fatal("opposite key orders did not share the same locks")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("opposite key orders deadlocked")
	}
}
