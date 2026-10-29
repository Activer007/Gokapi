//go:build test && security_whole

package storage

import (
	"errors"
	"math"
	"testing"

	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestSEC030WholeProcessingSpaceArithmetic(t *testing.T) {
	for _, bytes := range []int64{0, 1, 1 << 20, 2 << 30} {
		need, err := processingSpaceRequired(bytes, 400)
		minimum := uint64(bytes + encryption.CalculateEncryptedFilesize(bytes) + (400 << 20))
		if err != nil || need < minimum || need-minimum != 64<<10 {
			t.Fatal("checkpoint omitted simultaneous ciphertext or configured free reserve", bytes, err)
		}
	}
	for _, pair := range []struct {
		bytes int64
		floor int
	}{{-1, 0}, {0, -1}, {math.MaxInt64, 0}} {
		if _, err := processingSpaceRequired(pair.bytes, pair.floor); !errors.Is(err, uploadquota.ErrInvalid) {
			t.Fatal("invalid or overflowing disk checkpoint was accepted", err)
		}
	}
	if math.MaxInt > math.MaxInt32 {
		if _, err := processingSpaceRequired(1, math.MaxInt); !errors.Is(err, uploadquota.ErrInvalid) {
			t.Fatal("overflowing free-space floor was accepted", err)
		}
	}
}
