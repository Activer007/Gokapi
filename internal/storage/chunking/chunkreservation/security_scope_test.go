package chunkreservation

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSEC032CancelScope(t *testing.T) {
	for _, scenario := range []string{"own", "other-request", "missing", "expired", "expiry-boundary"} {
		t.Run(scenario, func(t *testing.T) {
			resetStateBlockCleanup()
			uuid := New("owner")
			id := "owner"
			switch scenario {
			case "other-request":
				id = "other"
			case "missing":
				uuid = "missing"
			case "expired", "expiry-boundary":
				r := reservedChunks[id][uuid]
				r.Expiry = time.Now().Unix()
				if scenario == "expired" {
					r.Expiry--
				}
				reservedChunks[id][uuid] = r
			}
			called := false
			if err := Cancel(id, uuid, func() error { called = true; return nil }); err != nil {
				t.Fatal(err)
			}
			if called != (scenario == "own") {
				t.Fatal("removal callback did not respect reservation scope/lifetime")
			}
			want := 1
			if scenario == "own" {
				want = 0
			}
			if GetCount("owner") != want {
				t.Fatal("unexpected reservation mutation")
			}
		})
	}
}

func TestSEC032CancelRetry(t *testing.T) {
	resetStateBlockCleanup()
	uuid := New("owner")
	want := errors.New("synthetic removal failure")
	if err := Cancel("owner", uuid, func() error { return want }); !errors.Is(err, want) {
		t.Fatal("removal failure lost")
	}
	if GetCount("owner") != 1 {
		t.Fatal("failed removal lost its reservation")
	}
	if err := Cancel("owner", uuid, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if GetCount("owner") != 0 {
		t.Fatal("retry did not release reservation")
	}
}

func TestSEC032CancelConcurrent(t *testing.T) {
	resetStateBlockCleanup()
	uuid := New("owner")
	var calls atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		wg.Go(func() {
			<-start
			if err := Cancel("owner", uuid, func() error { calls.Add(1); return nil }); err != nil {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 || GetCount("owner") != 0 {
		t.Fatal("concurrent cancellation did not remove exactly once")
	}
}
