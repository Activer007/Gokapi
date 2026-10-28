//go:build test

package fileupload

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/environment"
	"github.com/forceu/gokapi/internal/uploadquota"
	"github.com/forceu/gokapi/internal/webserver/errorHandling/errorcodes"
)

func TestSEC030WholeResponse(t *testing.T) {
	for _, tc := range []struct {
		name         string
		err          error
		status, code int
	}{
		{"logical", uploadquota.ErrQuota, http.StatusRequestEntityTooLarge, 21},
		{"active", uploadquota.ErrActive, http.StatusTooManyRequests, 22},
		{"staging", uploadquota.ErrStaging, http.StatusInsufficientStorage, errorcodes.CannotAllocateFile},
		{"receipts", uploadquota.ErrFull, http.StatusServiceUnavailable, 22},
		{"retry", uploadquota.ErrRetry, http.StatusServiceUnavailable, 22},
		{"conflict", uploadquota.ErrConflict, http.StatusConflict, errorcodes.ResourceCanNotBeEdited},
		{"provider", errors.New("synthetic-private-provider-path"), http.StatusInternalServerError, errorcodes.InternalServer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A provider can wrap an otherwise classifiable error with private
			// context. Only the fixed public response may cross the API boundary.
			err := wholeUploadFailure{fmt.Errorf("synthetic-private-key: %w", tc.err)}
			status, code, message := WholeUploadErrorResponse(err)
			if status != tc.status || code != tc.code || strings.Contains(message, "synthetic-private") {
				t.Fatalf("unsafe response: status=%d code=%d", status, code)
			}
			if !errors.Is(err, tc.err) {
				t.Fatal("opaque response wrapper lost internal error identity")
			}
		})
	}
	if !errors.Is(uploadquota.ErrActive, uploadquota.ErrQuota) || !errors.Is(uploadquota.ErrStaging, uploadquota.ErrQuota) {
		t.Fatal("capacity specialization lost legacy ErrQuota identity")
	}
}

func TestSEC030WholeLimits(t *testing.T) {
	env := environment.Environment{UploadUserTotalMB: 3, UploadUserMaxFiles: 5, UploadUserActive: 2,
		UploadInstanceTotalMB: 7, UploadInstanceMaxFiles: 11, UploadInstanceActive: 4, UploadStagingMB: 13}
	limits, err := wholeUploadLimits(env)
	if err != nil || limits.User.Bytes != 3<<20 || limits.User.Files != 5 || limits.Instance.Bytes != 7<<20 || limits.Instance.Files != 11 || limits.UserActive != 2 || limits.InstanceActive != 4 || limits.StagingBytes != 13<<20 {
		t.Fatal("whole upload limits did not preserve literal policy capacities")
	}
	env.UploadUserTotalMB = math.MaxInt64
	if _, err := wholeUploadLimits(env); err == nil {
		t.Fatal("overflowing MiB policy accepted")
	}
}
