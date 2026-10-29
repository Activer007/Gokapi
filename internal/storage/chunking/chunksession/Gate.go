package chunksession

import (
	"context"
	"errors"
	"regexp"
	"sync"

	"github.com/Activer007/SupaShare/internal/uploadquota"
)

var boundedID = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func validScope(ctx context.Context, scope Scope) error {
	if ctx == nil || scope.UserID <= 0 || len(scope.ClientID) < 10 || len(scope.ClientID) > 64<<10 || len(scope.RequestID) > uploadquota.MaxIDLength || (scope.RequestID != "" && !boundedID.MatchString(scope.RequestID)) {
		return uploadquota.ErrInvalid
	}
	if scope.RequestID != "" && (len(ID(scope)) > uploadquota.MaxIDLength || !boundedID.MatchString(scope.ClientID)) {
		return uploadquota.ErrInvalid
	}
	if len(ID(scope)) > uploadquota.MaxIDLength {
		return uploadquota.ErrInvalid
	}
	return ctx.Err()
}

type gate struct {
	token      chan struct{}
	claimed    chan struct{}
	refs       int
	completing bool
}

func (s *Store) acquire(ctx context.Context, id string, try bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if err := s.uncertain[id]; err != nil {
		s.mu.Unlock()
		return nil, errors.Join(uploadquota.ErrInProgress, err)
	}
	g := s.gates[id]
	if g == nil {
		if len(s.gates) >= uploadquota.MaxRecords {
			s.mu.Unlock()
			return nil, uploadquota.ErrFull
		}
		g = &gate{token: make(chan struct{}, 1), claimed: make(chan struct{})}
		s.gates[id] = g
	}
	if g.completing {
		s.mu.Unlock()
		return nil, uploadquota.ErrInProgress
	}
	g.refs++
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		g.refs--
		if g.refs == 0 {
			delete(s.gates, id)
		}
		s.mu.Unlock()
	}
	if try {
		select {
		case g.token <- struct{}{}:
		default:
			forget()
			return nil, uploadquota.ErrInProgress
		}
	} else {
		select {
		case g.token <- struct{}{}:
		case <-ctx.Done():
			forget()
			return nil, ctx.Err()
		case <-g.claimed:
			forget()
			return nil, uploadquota.ErrInProgress
		}
	}
	var once sync.Once
	release := func() { once.Do(func() { <-g.token; forget() }) }
	// A preceding owner may have encountered an uncertain close while this
	// waiter was queued. Its descriptor must not be assumed gone.
	s.mu.Lock()
	uncertain := s.uncertain[id]
	completing := g.completing
	s.mu.Unlock()
	if completing {
		release()
		return nil, uploadquota.ErrInProgress
	}
	if uncertain != nil {
		release()
		return nil, errors.Join(uploadquota.ErrInProgress, uncertain)
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// Notify both future and already queued callers that waiting for this lease
// cannot make a write/cancel valid. The persistent Claim is the restart guard.
func (s *Store) completing(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gates[id]
	if !g.completing {
		g.completing = true
		close(g.claimed)
	}
}
