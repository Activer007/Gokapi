//go:build test

package aws

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

// This boundary must compile and fail closed in SDK, awsmock and noaws builds.
func TestM5StreamContextCanceledBoundary(t *testing.T) {
	if err := StreamContext(nil, io.Discard, models.File{}); err == nil {
		t.Fatal("missing streaming context was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := StreamContext(ctx, io.Discard, models.File{}); !errors.Is(err, context.Canceled) {
		t.Fatal("streaming boundary did not preserve caller cancellation")
	}
}
