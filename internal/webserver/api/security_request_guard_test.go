//go:build test

package api

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/errorHandling/errorcodes"
)

// The same reservation gate must precede synchronous work and background
// scheduling. Detailed end-to-end evidence is retained in the private review.
func TestSEC032CompletionGate(t *testing.T) {
	request := models.FileRequest{Id: rand.Text(), UserId: 9032, ApiKey: rand.Text()}
	database.SaveFileRequest(request)
	t.Cleanup(func() { database.DeleteFileRequest(request) })
	for _, async := range []bool{false, true} {
		w := httptest.NewRecorder()
		apiChunkUploadRequestComplete(w, &paramChunkUploadRequestComplete{
			FileRequestId: request.Id, Uuid: rand.Text(), IsNonBlocking: async,
		}, models.User{Id: request.UserId}, models.ApiKey{Id: request.ApiKey})
		var result struct{ ErrorCode int }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusBadRequest || result.ErrorCode != errorcodes.InvalidChunkReservation {
			t.Errorf("nonblocking=%v: missing reservation must fail before processing, status=%d code=%d", async, w.Code, result.ErrorCode)
		}
	}
}
