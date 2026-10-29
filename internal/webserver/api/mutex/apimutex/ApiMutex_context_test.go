package apimutex

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSEC040MutexContextLegacyCompatibility(t *testing.T) {
	for _, legacy := range []string{"Lock", "LockMany"} {
		t.Run(legacy, func(t *testing.T) {
			const key = "context-legacy-held"
			var release func()
			if legacy == "Lock" {
				Lock(TypeMetaData, key)
				release = func() { Unlock(TypeMetaData, key) }
			} else {
				release = LockMany(TypeMetaData, key)
			}
			defer release()
			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()
			var unlock func()
			var err error
			if legacy == "Lock" {
				unlock, err = LockManyContext(ctx, TypeMetaData, key)
			} else {
				unlock, err = LockContext(ctx, TypeMetaData, key)
			}
			if unlock != nil {
				unlock()
				t.Fatal("context lock bypassed a held legacy stripe")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("legacy stripe did not block the context lock until its deadline", err)
			}
		})
	}
}

func TestSEC040MutexContextCancellationReleasesPartial(t *testing.T) {
	low, high := "context-partial-a", "context-partial-b"
	for i := 0; i < 4096 && stripeIndex(TypeMetaData, low) == stripeIndex(TypeMetaData, high); i++ {
		high = fmt.Sprintf("context-partial-%d", i)
	}
	if stripeIndex(TypeMetaData, low) == stripeIndex(TypeMetaData, high) {
		t.Fatal("distinct stripe fixture missing")
	}
	if stripeIndex(TypeMetaData, low) > stripeIndex(TypeMetaData, high) {
		low, high = high, low
	}
	Lock(TypeMetaData, high)
	defer Unlock(TypeMetaData, high)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	// Input is reversed. The lower free stripe must be acquired before the
	// higher held stripe, then released when that second acquisition expires.
	unlock, err := LockManyContext(ctx, TypeMetaData, high, low)
	if unlock != nil {
		unlock()
		t.Fatal("multi-lock bypassed the held later stripe")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("partial acquisition did not return its deadline", err)
	}
	retry, cancelRetry := context.WithTimeout(t.Context(), time.Second)
	defer cancelRetry()
	release, err := LockContext(retry, TypeMetaData, low)
	if err != nil {
		t.Fatal("cancellation leaked an earlier stripe", err)
	}
	release()
}

func TestSEC040MutexContextCollisionDeduplication(t *testing.T) {
	const key = "context-collision-anchor"
	collision := ""
	for i := 0; i < 4096; i++ {
		candidate := fmt.Sprintf("context-collision-%d", i)
		if stripeIndex(TypeMetaData, candidate) == stripeIndex(TypeMetaData, key) {
			collision = candidate
			break
		}
	}
	if collision == "" {
		t.Fatal("deterministic collision fixture missing")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	unlock, err := LockManyContext(ctx, TypeMetaData, key, key, collision)
	if err != nil {
		t.Fatal("duplicate keys or colliding stripes self-deadlocked", err)
	}
	unlock()
	// A fresh acquisition must remain possible after the deduplicated release.
	unlock, err = LockContext(ctx, TypeMetaData, collision)
	if err != nil {
		t.Fatal("deduplicated release leaked its stripe", err)
	}
	unlock()
}

func TestSEC040MutexContextValidation(t *testing.T) {
	calls := []struct {
		name string
		lock func(context.Context) (func(), error)
	}{
		{"single", func(ctx context.Context) (func(), error) { return LockContext(ctx, TypeMetaData, "context-validation") }},
		{"many", func(ctx context.Context) (func(), error) {
			return LockManyContext(ctx, TypeMetaData, "context-validation")
		}},
		{"empty", func(ctx context.Context) (func(), error) { return LockManyContext(ctx, TypeMetaData) }},
	}
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			if unlock, err := call.lock(nil); err == nil || unlock != nil {
				if unlock != nil {
					unlock()
				}
				t.Fatal("nil context was accepted")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if unlock, err := call.lock(ctx); !errors.Is(err, context.Canceled) || unlock != nil {
				if unlock != nil {
					unlock()
				}
				t.Fatal("already canceled context acquired a stripe", err)
			}
		})
	}
	unlock, err := LockManyContext(t.Context(), TypeMetaData)
	if err != nil || unlock == nil {
		t.Fatal("empty live acquisition did not return a no-op unlock", err)
	}
	unlock()
}

func TestSEC040MutexFileRequestDomainIsolation(t *testing.T) {
	const key = "context-file-request-held"
	collision := ""
	for i := 0; i < 4096; i++ {
		candidate := fmt.Sprintf("context-file-request-metadata-%d", i)
		if stripeIndex(TypeMetaData, candidate) == stripeIndex(TypeFileRequest, key) {
			collision = candidate
			break
		}
	}
	if collision == "" {
		t.Fatal("deterministic cross-domain collision fixture missing")
	}
	Lock(TypeFileRequest, key)
	held := true
	defer func() {
		if held {
			Unlock(TypeFileRequest, key)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	// A request lifecycle gate can be held while its files are locked, even
	// when those file IDs have the same numeric stripe index or identical key.
	metadataUnlock, err := LockManyContext(ctx, TypeMetaData, key, collision)
	if err != nil {
		t.Fatal("request gate blocked the independent metadata domain", err)
	}
	metadataUnlock()
	wait, cancelWait := context.WithTimeout(t.Context(), time.Second)
	defer cancelWait()
	cancelTimer := time.AfterFunc(50*time.Millisecond, cancelWait)
	defer cancelTimer.Stop()
	unlock, err := LockManyContext(wait, TypeFileRequest, key, key)
	if unlock != nil {
		unlock()
		t.Fatal("same request bypassed its held lifecycle gate")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("request gate did not honor its waiting context", err)
	}
	Unlock(TypeFileRequest, key)
	held = false
	retry, cancelRetry := context.WithTimeout(t.Context(), time.Second)
	defer cancelRetry()
	unlock, err = LockContext(retry, TypeFileRequest, key)
	if err != nil {
		t.Fatal("canceled request acquisition leaked its gate", err)
	}
	unlock()
}
