//go:build test

package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/helper"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/webserver/errorHandling/errorcodes"
)

// The same reservation gate must precede synchronous work and background
// scheduling. Detailed end-to-end evidence is retained in the private review.
func TestSEC032CompletionGate(t *testing.T) {
	user, found := database.GetUser(idUser)
	if !found {
		t.Fatal("missing current owner fixture")
	}
	request := models.FileRequest{Id: rand.Text(), UserId: user.Id, ApiKey: rand.Text()}
	database.SaveFileRequest(request)
	t.Cleanup(func() { database.DeleteFileRequest(request) })
	for _, async := range []bool{false, true} {
		beforeFiles := database.GetAllMetadata()
		beforeLedger, err := database.ReadUploadQuota(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		parameter := &paramChunkUploadRequestComplete{
			FileRequestId: request.Id, Uuid: helper.GenerateRandomString(32), IsNonBlocking: async,
			FileName: "synthetic.bin", ContentType: "application/octet-stream", FileSize: 0,
		}
		if err := parameter.ProcessParameter(nil); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		apiChunkUploadRequestComplete(w, parameter, user, models.ApiKey{Id: request.ApiKey, UserId: user.Id, UploadRequestId: request.Id})
		var result struct{ ErrorCode int }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusBadRequest || result.ErrorCode != errorcodes.InvalidChunkReservation {
			t.Errorf("nonblocking=%v: missing reservation must fail before processing, status=%d code=%d", async, w.Code, result.ErrorCode)
		}
		afterLedger, err := database.ReadUploadQuota(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(beforeFiles, database.GetAllMetadata()) || !reflect.DeepEqual(beforeLedger, afterLedger) {
			t.Fatal("missing request reservation created metadata or a completion claim")
		}
	}
}
