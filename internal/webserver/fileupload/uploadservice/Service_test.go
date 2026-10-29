package uploadservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func serviceTestStart(t *testing.T, parent context.Context) *Generation {
	t.Helper()
	generation := Start(parent)
	t.Cleanup(func() {
		generation.BeginShutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := generation.Wait(ctx); err != nil {
			t.Error("service cleanup did not drain admitted operations")
		}
		Start(context.Background())
	})
	return generation
}

func serviceTestAcquire(t *testing.T, generation *Generation, request context.Context) *Ticket {
	t.Helper()
	ticket, err := generation.Acquire(request)
	if err != nil || ticket == nil {
		t.Fatal("valid request did not acquire an upload lifetime ticket")
	}
	t.Cleanup(ticket.Done)
	return ticket
}

func serviceTestCanceled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Error("operation context did not observe cancellation")
	}
}

func TestSEC030UploadServiceCurrentFallback(t *testing.T) {
	generation := Current()
	if generation == nil || generation.Context() == nil {
		t.Fatal("direct API tests have no initial service generation")
	}
	ticket := serviceTestAcquire(t, generation, context.Background())
	ticket.Done()
}

func TestSEC030UploadServiceRequestAndDetached(t *testing.T) {
	generation := serviceTestStart(t, context.Background())
	type requestKey struct{}
	request, cancelRequest := context.WithCancel(context.WithValue(context.Background(), requestKey{}, "synthetic-value"))
	defer cancelRequest()
	ticket := serviceTestAcquire(t, generation, request)
	if ticket.Context().Value(requestKey{}) != "synthetic-value" {
		t.Error("synchronous operation lost request context values")
	}
	deadline := time.Now().Add(time.Minute)
	detached, cancelDetached := ticket.Detached(deadline)
	defer cancelDetached()
	if actual, ok := detached.Deadline(); !ok || !actual.Equal(deadline) {
		t.Error("detached operation lost its finite completion deadline")
	}
	cancelRequest()
	serviceTestCanceled(t, ticket.Context())
	select {
	case <-detached.Done():
		t.Error("HTTP cancellation terminated the already admitted detached operation")
	default:
	}
	if generation.Context().Err() != nil {
		t.Error("one request cancellation terminated the service")
	}
	ticket.Done()
	serviceTestCanceled(t, detached)
	if next, err := generation.Acquire(context.Background()); err != nil {
		t.Error("finishing one ticket closed the service generation")
	} else {
		next.Done()
	}
}

func TestSEC030UploadServiceShutdownDrains(t *testing.T) {
	generation := serviceTestStart(t, context.Background())
	ticket := serviceTestAcquire(t, generation, context.Background())
	detached, cancel := ticket.Detached(time.Now().Add(time.Minute))
	defer cancel()
	generation.BeginShutdown()
	serviceTestCanceled(t, ticket.Context())
	serviceTestCanceled(t, detached)
	if acquired, err := generation.Acquire(context.Background()); err == nil {
		if acquired != nil {
			acquired.Done()
		}
		t.Error("shutdown accepted a new operation")
	}
	ctx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if err := generation.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Error("Wait completed before the admitted ticket was explicitly finished")
	}
	ticket.Done()
	ticket.Done()
	ctx, stopDrain := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopDrain()
	if err := generation.Wait(ctx); err != nil {
		t.Error("Wait did not finish after the last ticket completed")
	}
}

func TestSEC030UploadServiceReplacement(t *testing.T) {
	old := serviceTestStart(t, context.Background())
	oldTicket := serviceTestAcquire(t, old, context.Background())
	oldDetached, cancelOld := oldTicket.Detached(time.Now().Add(time.Minute))
	defer cancelOld()
	current := serviceTestStart(t, context.Background())
	if Current() != current || current == old {
		t.Error("Start did not install a fresh generation")
	}
	serviceTestCanceled(t, old.Context())
	serviceTestCanceled(t, oldDetached)
	newTicket := serviceTestAcquire(t, current, context.Background())
	old.BeginShutdown()
	oldTicket.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := old.Wait(ctx); err != nil {
		t.Error("previous generation could not drain")
	}
	if current.Context().Err() != nil || newTicket.Context().Err() != nil {
		t.Error("stopping an old generation canceled its replacement")
	}
}

func TestSEC030UploadServiceDoneIdempotent(t *testing.T) {
	generation := serviceTestStart(t, context.Background())
	ticket := serviceTestAcquire(t, generation, context.Background())
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() { defer workers.Done(); ticket.Done() }()
	}
	workers.Wait()
	serviceTestCanceled(t, ticket.Context())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := generation.Wait(ctx); err != nil {
		t.Error("idempotent ticket completion did not balance service admission")
	}
}

func TestSEC030UploadServiceCanceledAcquireAndDeadline(t *testing.T) {
	generation := serviceTestStart(t, context.Background())
	request, cancel := context.WithCancel(context.Background())
	cancel()
	if ticket, err := generation.Acquire(request); !errors.Is(err, context.Canceled) || ticket != nil {
		if ticket != nil {
			ticket.Done()
		}
		t.Error("already canceled request was added to service admission")
	}
	if ticket, err := generation.Acquire(nil); err == nil || ticket != nil {
		if ticket != nil {
			ticket.Done()
		}
		t.Error("nil request context acquired a lifetime ticket")
	}
	ticket := serviceTestAcquire(t, generation, context.Background())
	detached, stop := ticket.Detached(time.Now().Add(50 * time.Millisecond))
	defer stop()
	serviceTestCanceled(t, detached)
	if !errors.Is(detached.Err(), context.DeadlineExceeded) {
		t.Error("finite detached deadline was not enforced")
	}
	if ticket.Context().Err() != nil {
		t.Error("one detached deadline canceled its synchronous parent")
	}
}

func TestSEC030UploadServiceParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	generation := serviceTestStart(t, parent)
	ticket := serviceTestAcquire(t, generation, context.Background())
	detached, stop := ticket.Detached(time.Now().Add(time.Minute))
	defer stop()
	cancel()
	serviceTestCanceled(t, ticket.Context())
	serviceTestCanceled(t, detached)
	if next, err := generation.Acquire(context.Background()); err == nil {
		if next != nil {
			next.Done()
		}
		t.Error("canceled service parent still accepted new operations")
	}
}

func TestSEC030UploadServiceConcurrentAcquireShutdown(t *testing.T) {
	for range 20 {
		generation := serviceTestStart(t, context.Background())
		start := make(chan struct{})
		var workers sync.WaitGroup
		for range 32 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				for range 8 {
					ticket, err := generation.Acquire(context.Background())
					if err == nil {
						if ticket == nil {
							t.Error("successful admission returned no ticket")
							return
						}
						ticket.Done()
					}
				}
			}()
		}
		close(start)
		generation.BeginShutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := generation.Wait(ctx); err != nil {
			t.Error("concurrent stop/admission did not drain")
		}
		cancel()
		workers.Wait()
		if ticket, err := generation.Acquire(context.Background()); err == nil {
			if ticket != nil {
				ticket.Done()
			}
			t.Error("completed shutdown reopened admission")
		}
	}
}
