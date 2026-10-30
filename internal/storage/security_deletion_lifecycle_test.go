//go:build test && noaws && security_deletion

package storage

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
)

func TestSEC040DeletionBeforeDeadline(t *testing.T) {
	dir := deletionStateStore(t)
	file := deletionStateFile(t, dir, "state-future-deadline")
	plan, err := ScheduleFileDeletionContext(t.Context(), file, int(time.Hour/time.Millisecond), true)
	if err != nil || plan.State != DeletionAccepted {
		t.Fatal("could not schedule the future deletion", err)
	}
	expected := file
	expected.PendingDeletion = plan.File.PendingDeletion
	if expected.PendingDeletion <= time.Now().Unix() || !reflect.DeepEqual(plan.File, expected) {
		t.Fatal("scheduling did not preserve the pending share's unrelated fields")
	}
	deletionTimers.mu.Lock()
	entry := deletionTimers.entries[file.Id]
	var token *deletionTimerToken
	if entry != nil {
		token = entry.token
	}
	deletionTimers.mu.Unlock()
	if token == nil {
		t.Fatal("scheduled deletion lost its live timer")
	}
	// Invoke the real callback with its current token before its exact deadline.
	// Neither this early invocation nor a GC pass may retire the pending share.
	executeDeletionTimer(file.Id, token)
	result, err := CleanUpContext(t.Context())
	if err != nil || result.Retired != 0 {
		t.Fatal("cleanup retired a future pending deletion", err)
	}
	stored, found, err := database.ReadRecoveryFile(t.Context(), file.Id)
	if err != nil || !found || !reflect.DeepEqual(stored, plan.File) {
		t.Fatal("early cleanup changed the pending metadata", err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, file.SHA1)); err != nil || string(content) != "abc" {
		t.Fatal("early cleanup changed the source", err)
	}
	if id, found := database.GetHotlink(file.HotlinkId); !found || id != file.Id {
		t.Fatal("early cleanup retired the hotlink")
	}
}

func TestSEC040DeletionQueuedCallbackAfterRestore(t *testing.T) {
	dir := deletionStateStore(t)
	file := deletionStateFile(t, dir, "state-queued-restore")
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	previous := deletionTimers
	registry := newDeletionTimerRegistry(1, func(id string, token *deletionTimerToken) {
		close(entered)
		<-release
		executeDeletionTimer(id, token)
		close(done)
	})
	deletionTimers = registry
	t.Cleanup(func() {
		defer func() { deletionTimers = previous }()
		unblock()
		registry.invalidate(file.Id)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		// The callback's done signal precedes fire's deferred finish. Drain the
		// registry too before restoring globals or closing the fixture database.
		for {
			registry.mu.Lock()
			remaining := len(registry.entries)
			registry.mu.Unlock()
			if remaining == 0 {
				break
			}
			select {
			case <-tick.C:
			case <-ctx.Done():
				t.Error("queued deletion callback did not drain")
				return
			}
		}
	})
	plan, err := ScheduleFileDeletionContext(t.Context(), file, 0, true)
	if err != nil || plan.State != DeletionAccepted {
		t.Fatal("could not schedule the queued callback", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("timer callback never entered")
	}
	restored, err := CancelFileDeletionContext(ctx, plan.File)
	if err != nil || !reflect.DeepEqual(restored, file) {
		t.Fatal("restore did not preserve the live share", err)
	}
	unblock()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("stale callback did not finish after restore")
	}
	stored, found, err := database.ReadRecoveryFile(ctx, file.Id)
	if err != nil || !found || !reflect.DeepEqual(stored, file) {
		t.Fatal("queued callback changed restored metadata", err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, file.SHA1)); err != nil || string(content) != "abc" {
		t.Fatal("queued callback deleted the restored source", err)
	}
	if id, found := database.GetHotlink(file.HotlinkId); !found || id != file.Id {
		t.Fatal("queued callback retired the restored hotlink")
	}
}
