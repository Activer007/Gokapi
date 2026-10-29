//go:build test && security_deletion

package storage

import (
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestSEC040DeletionTimerCapacityAndAbort(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		registry := newDeletionTimerRegistry(1, func(string, *deletionTimerToken) {
			t.Error("cancelled timer executed")
		})
		first, err := registry.reserve("first")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := registry.reserve("second"); !errors.Is(err, errDeletionTimerCapacity) {
			t.Fatal("reservation did not consume capacity")
		}
		if _, err := registry.reserve("first"); !errors.Is(err, errDeletionTimerReserved) {
			t.Fatal("overlapping reservation accepted")
		}
		first.abort()
		first.abort()
		second, err := registry.reserve("second")
		if err != nil {
			t.Fatal("abort did not release capacity")
		}
		second.commit(deletionTimerSpec{File: models.File{Id: "second"}, Deadline: time.Now().Add(time.Hour)})
		second.abort()
		replacement, err := registry.reserve("second")
		if err != nil {
			t.Fatal(err)
		}
		replacement.abort()
		if _, current := registry.current("second", second.token); !current {
			t.Fatal("aborted replacement lost the previous intent")
		}
		if _, err := registry.reserve("first"); !errors.Is(err, errDeletionTimerCapacity) {
			t.Fatal("abort after commit cancelled a live timer")
		}
		registry.invalidate("second")
		reused, err := registry.reserve("first")
		if err != nil {
			t.Fatal("stopped timer did not release capacity")
		}
		registry.invalidate("first")
		reused.commit(deletionTimerSpec{File: models.File{Id: "first"}, Deadline: time.Now()})
		synctest.Wait()
	})
}

func TestSEC040DeletionTimerRunningCancellationIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var registry *deletionTimerRegistry
		var calls, active, peak atomic.Int32
		entered := make(chan *deletionTimerToken, 2)
		release := make(chan struct{})
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		observed := make(chan bool, 2)
		registry = newDeletionTimerRegistry(1, func(id string, token *deletionTimerToken) {
			count := active.Add(1)
			defer active.Add(-1)
			for previous := peak.Load(); count > previous && !peak.CompareAndSwap(previous, count); previous = peak.Load() {
			}
			call := calls.Add(1)
			entered <- token
			if call == 1 {
				<-release
			}
			_, current := registry.current(id, token)
			observed <- current
		})
		first, err := registry.reserve("same")
		if err != nil {
			t.Fatal(err)
		}
		first.commit(deletionTimerSpec{File: models.File{Id: "same"}, Deadline: time.Now()})
		synctest.Wait()
		var oldToken *deletionTimerToken
		select {
		case oldToken = <-entered:
		default:
			t.Fatal("initial callback did not start")
		}
		registry.invalidate("same")
		if _, err := registry.reserve("other"); !errors.Is(err, errDeletionTimerCapacity) {
			t.Fatal("running cancelled callback lost its capacity charge")
		}
		for i := 0; i < 32; i++ {
			reservation, err := registry.reserve("same")
			if err != nil {
				t.Fatal(err)
			}
			reservation.commit(deletionTimerSpec{File: models.File{Id: "same"}, Deadline: time.Now()})
			registry.invalidate("same")
		}
		latest, err := registry.reserve("same")
		if err != nil {
			t.Fatal(err)
		}
		latest.commit(deletionTimerSpec{File: models.File{Id: "same", PendingDeletion: 7}, Deadline: time.Now()})
		synctest.Wait()
		if calls.Load() != 1 || peak.Load() != 1 {
			t.Fatal("replanning started another callback before the first returned")
		}
		if _, current := registry.current("same", oldToken); current {
			t.Fatal("old callback token became current again")
		}
		if spec, current := registry.current("same", latest.token); !current || spec.File.PendingDeletion != 7 {
			t.Fatal("latest intent was not retained")
		}
		close(release)
		synctest.Wait()
		if calls.Load() != 2 || len(observed) != 2 || len(entered) != 1 {
			t.Fatal("replacement callback did not finish after the old callback")
		}
		if <-observed {
			t.Fatal("old callback was allowed to execute the replacement intent")
		}
		if !<-observed || calls.Load() != 2 || peak.Load() != 1 {
			t.Fatal("replacement callback was not executed exactly once after the old callback")
		}
		if <-entered != latest.token {
			t.Fatal("callback used an intermediate replanned intent")
		}
		reused, err := registry.reserve("other")
		if err != nil {
			t.Fatal("finished callbacks did not release capacity")
		}
		reused.abort()
	})
}

func TestSEC040DeletionTimerReservationSurvivesFinish(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		var calls atomic.Int32
		registry := newDeletionTimerRegistry(1, func(string, *deletionTimerToken) {
			if calls.Add(1) == 1 {
				<-release
			}
		})
		first, err := registry.reserve("same")
		if err != nil {
			t.Fatal(err)
		}
		first.commit(deletionTimerSpec{File: models.File{Id: "same"}, Deadline: time.Now()})
		synctest.Wait()
		pending, err := registry.reserve("same")
		if err != nil {
			t.Fatal(err)
		}
		close(release)
		synctest.Wait()
		if _, err := registry.reserve("other"); !errors.Is(err, errDeletionTimerCapacity) {
			t.Fatal("callback finish dropped an outstanding reservation")
		}
		pending.commit(deletionTimerSpec{File: models.File{Id: "same"}, Deadline: time.Now().Add(time.Second)})
		time.Sleep(time.Second)
		synctest.Wait()
		if calls.Load() != 2 {
			t.Fatal("reservation was lost when the previous callback finished")
		}
	})
}
