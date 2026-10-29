// Package uploadservice tracks the lifetime of admitted upload operations.
// Durable quotas and completion claims remain owned by the upload ledger.
package uploadservice

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrClosing means the service generation no longer admits upload operations.
var ErrClosing = errors.New("upload service is shutting down")

// ErrInvalidContext means an operation was called without a context.
var ErrInvalidContext = errors.New("invalid upload context")

// Generation owns cancellation and drain accounting for one server lifetime.
// Create independent generations with New, or install one with Start. Retain
// the returned snapshot during shutdown.
type Generation struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	closing  bool
	admitted sync.WaitGroup
	waitOnce sync.Once
	drained  chan struct{}
}

// Ticket accounts for one admitted operation until its caller invokes Done.
// Cancellation does not imply that persistent work has finished.
type Ticket struct {
	generation     *Generation
	ctx            context.Context
	cancel         context.CancelFunc
	lifetime       context.Context
	cancelLifetime context.CancelFunc
	stopLifetime   func() bool
	once           sync.Once
}

var currentMu sync.RWMutex
var current = New(context.Background())

// New constructs an independent lifetime without replacing Current. As with
// context.WithCancel, parent must be non-nil.
func New(parent context.Context) *Generation {
	ctx, cancel := context.WithCancel(parent)
	return &Generation{ctx: ctx, cancel: cancel, drained: make(chan struct{})}
}

// Start installs a fresh server lifetime and stops admission in its predecessor.
// It does not wait for the predecessor's existing tickets to finish.
// As with context.WithCancel, parent must be non-nil.
func Start(parent context.Context) *Generation {
	generation := New(parent)
	currentMu.Lock()
	previous := current
	current = generation
	currentMu.Unlock()
	previous.BeginShutdown()
	return generation
}

// Current returns the current generation, including the initial test fallback.
func Current() *Generation {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return current
}

// Context is canceled when this generation shuts down or its parent is canceled.
func (g *Generation) Context() context.Context { return g.ctx }

// Acquire tracks an operation while retaining its request context values.
// Admission and shutdown share a mutex so Wait never races a later Add.
func (g *Generation) Acquire(request context.Context) (*Ticket, error) {
	if request == nil {
		return nil, ErrInvalidContext
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := request.Err(); err != nil {
		return nil, err
	}
	if g.closing || g.ctx.Err() != nil {
		return nil, ErrClosing
	}

	lifetime, cancelLifetime := context.WithCancel(g.ctx)
	ctx, cancel := context.WithCancel(request)
	ticket := &Ticket{
		generation:     g,
		ctx:            ctx,
		cancel:         cancel,
		lifetime:       lifetime,
		cancelLifetime: cancelLifetime,
	}
	ticket.stopLifetime = context.AfterFunc(lifetime, cancel)
	if lifetime.Err() != nil {
		cancel()
	}
	g.admitted.Add(1)
	return ticket, nil
}

// BeginShutdown rejects new operations and cancels existing operation contexts.
// Existing tickets remain accounted for until Done confirms they have finished.
func (g *Generation) BeginShutdown() {
	g.mu.Lock()
	g.closing = true
	g.mu.Unlock()
	g.cancel()
}

// Wait begins shutdown and waits for all tickets, bounded by the caller's context.
// Each generation starts at most one drain waiter, even when callers time out.
func (g *Generation) Wait(ctx context.Context) error {
	g.BeginShutdown()
	if ctx == nil {
		return ErrInvalidContext
	}
	g.waitOnce.Do(func() {
		go func() {
			g.admitted.Wait()
			close(g.drained)
		}()
	})
	select {
	case <-g.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Context observes both the original request and the service lifetime.
func (ticket *Ticket) Context() context.Context { return ticket.ctx }

// Detached gives claimed asynchronous work a finite service-bound deadline.
// Ending the HTTP request does not cancel this context; Done and shutdown do.
func (ticket *Ticket) Detached(deadline time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ticket.lifetime, deadline)
}

// Done releases this ticket once, after all synchronous or detached work ends.
func (ticket *Ticket) Done() {
	ticket.once.Do(func() {
		ticket.stopLifetime()
		ticket.cancel()
		ticket.cancelLifetime()
		ticket.generation.admitted.Done()
	})
}
