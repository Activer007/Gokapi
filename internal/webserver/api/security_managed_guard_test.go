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
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/webserver/errorHandling/errorcodes"
)

func TestSEC032ManagedCompletionGate(t *testing.T) {
	for _, async := range []bool{false, true} {
		beforeFiles := database.GetAllMetadata()
		beforeLedger, err := database.ReadUploadQuota(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		request := &paramChunkComplete{Uuid: rand.Text(), FileName: "synthetic.bin", ContentType: "application/octet-stream", FileSize: 0, IsNonBlocking: async}
		if err := request.ProcessParameter(nil); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		apiChunkComplete(w, request, models.User{Id: 9132}, models.ApiKey{})
		var result struct{ ErrorCode int }
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusBadRequest || result.ErrorCode != errorcodes.InvalidChunkReservation {
			t.Errorf("nonblocking=%v: missing owned chunk must be rejected before processing, status=%d code=%d", async, w.Code, result.ErrorCode)
		}
		afterLedger, err := database.ReadUploadQuota(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(beforeFiles, database.GetAllMetadata()) || !reflect.DeepEqual(beforeLedger, afterLedger) {
			t.Fatal("missing owned upload created metadata or claimed a reservation")
		}
	}
}
