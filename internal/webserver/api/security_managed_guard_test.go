//go:build test

package api

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forceu/gokapi/internal/models"
)

func TestSEC032ManagedCompletionGate(t *testing.T) {
	for _, async := range []bool{false, true} {
		w := httptest.NewRecorder()
		apiChunkComplete(w, &paramChunkComplete{Uuid: rand.Text(), IsNonBlocking: async}, models.User{Id: 9132}, models.ApiKey{})
		if w.Code != http.StatusBadRequest {
			t.Errorf("nonblocking=%v: missing owned chunk must be rejected before processing, got %d", async, w.Code)
		}
	}
}
