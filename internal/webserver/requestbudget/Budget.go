// Package requestbudget bounds management I/O and propagates cancellation without
// buffering responses or moving handlers into detached goroutines.
package requestbudget

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Management applies a request-wide network and context budget. It must be placed
// outside response wrappers, so ResponseController reaches the server writer.
// It cannot preempt CPU work or database/storage calls that ignore Context.
func Management(next http.Handler, budget time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), budget)
		defer cancel()
		if ctx.Err() != nil {
			return
		}
		deadline, _ := ctx.Deadline()
		controller := http.NewResponseController(w)
		for _, setDeadline := range []func(time.Time) error{controller.SetReadDeadline, controller.SetWriteDeadline} {
			if err := setDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
				http.Error(w, "Unable to set request deadline", http.StatusServiceUnavailable)
				return
			}
		}
		// Non-network ResponseWriters (e.g. recorders) may not support deadlines; the
		// Context budget still applies. Real HTTP/1 and HTTP/2 server writers do.
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
