//go:build test

package api

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestCollectionPolicyDefaultsAndUpdates(t *testing.T) {
	user, ok := database.GetUser(idSuperAdmin)
	if !ok {
		t.Fatal("missing fixture owner")
	}
	key := models.ApiKey{Id: rand.Text(), UserId: user.Id, Permissions: models.ApiPermManageFileRequests}
	database.SaveApiKey(key)
	t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	call := func(headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/uploadrequest/save", nil)
		r.Header.Set("apikey", key.Id)
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		Process(w, r)
		return w
	}
	read := func(w *httptest.ResponseRecorder) models.FileRequest {
		t.Helper()
		var request models.FileRequest
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &request) != nil || request.Id == "" {
			t.Fatalf("save failed: status=%d body=%s", w.Code, w.Body.String())
		}
		return request
	}
	before := time.Now().Unix()
	request := read(call(nil))
	t.Cleanup(func() {
		database.DeleteFileRequest(request)
		database.DeleteApiKey(request.ApiKey)
	})
	if request.AllowedDownloads != 1 || request.Expiry < before+5*24*60*60 || request.Expiry > time.Now().Unix()+5*24*60*60 {
		t.Fatalf("unexpected creation defaults: downloads=%d expiry=%d", request.AllowedDownloads, request.Expiry)
	}
	for _, value := range []string{"0", "-1", "11", "1.5", "bogus"} {
		w := call(map[string]string{"id": request.Id, "allowedDownloads": value})
		if w.Code != http.StatusBadRequest {
			t.Errorf("allowedDownloads=%q status=%d, want 400", value, w.Code)
		}
		stored, _ := database.GetFileRequest(request.Id)
		if stored.AllowedDownloads != 1 || stored.Expiry != request.Expiry {
			t.Fatal("invalid update changed the saved request")
		}
	}
	updated := read(call(map[string]string{"id": request.Id, "allowedDownloads": "10"}))
	if updated.AllowedDownloads != 10 || updated.Expiry != request.Expiry {
		t.Fatal("explicit download policy update changed the deadline")
	}
	// A rename of an old request must not turn unlimited existing policy into 1.
	updated.AllowedDownloads, updated.Expiry = 0, 0
	database.SaveFileRequest(updated)
	updated = read(call(map[string]string{"id": request.Id, "name": "Renamed legacy collection"}))
	if updated.AllowedDownloads != 0 || updated.Expiry != 0 {
		t.Fatal("omitted fields replaced a legacy unlimited policy")
	}
	updated = read(call(map[string]string{"id": request.Id, "allowedDownloads": "1"}))
	if updated.AllowedDownloads != 1 {
		t.Fatal("legacy request cannot opt into the minimum download limit")
	}
	countBefore := len(database.GetAllFileRequests())
	if w := call(map[string]string{"allowedDownloads": "0"}); w.Code != http.StatusBadRequest || len(database.GetAllFileRequests()) != countBefore {
		t.Fatal("invalid creation persisted a collection")
	}
	explicit := read(call(map[string]string{"allowedDownloads": "10", "expiry": "0"}))
	t.Cleanup(func() {
		database.DeleteFileRequest(explicit)
		database.DeleteApiKey(explicit.ApiKey)
	})
	if explicit.AllowedDownloads != 10 || explicit.Expiry != 0 {
		t.Fatal("explicit creation policy was replaced by defaults")
	}
}
