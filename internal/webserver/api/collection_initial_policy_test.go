//go:build test

package api

import (
	"crypto/rand"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
)

// The empty management page must use these limits rather than weakening the
// creation API. Administrators bypass guest caps; zero disables each cap alone.
func TestCollectionInitialPolicyAPI(t *testing.T) {
	originalUser, ok := database.GetUser(idUser)
	if !ok {
		t.Fatal("missing normal-user fixture")
	}
	user := originalUser
	user.GrantPermission(models.UserPermGuestUploads)
	database.SaveUser(user, false)
	t.Cleanup(func() { database.SaveUser(originalUser, false) })
	keys := make(map[int]string)
	for _, owner := range []int{idUser, idAdmin, idSuperAdmin} {
		key := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: owner, Permissions: models.ApiPermManageFileRequests}
		database.SaveApiKey(key)
		keys[owner] = key.Id
		t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	}
	requestIDs := func() map[string]struct{} {
		ids := make(map[string]struct{})
		for _, request := range database.GetAllFileRequests() {
			ids[request.Id] = struct{}{}
		}
		return ids
	}
	keyIDs := func() map[string]struct{} {
		ids := make(map[string]struct{})
		for id := range database.GetAllApiKeys() {
			ids[id] = struct{}{}
		}
		return ids
	}
	type creationCase struct {
		name        string
		owner       int
		files, size string // Empty means the header is omitted.
		allowed     bool
	}
	for _, scenario := range []struct {
		name        string
		files, size int
		cases       []creationCase
	}{
		{"both-capped", 3, 5, []creationCase{
			{"omitted", idUser, "", "", false},
			{"files-omitted", idUser, "", "5", false},
			{"size-omitted", idUser, "3", "", false},
			{"zero", idUser, "0", "0", false},
			{"files-zero", idUser, "0", "5", false},
			{"size-zero", idUser, "3", "0", false},
			{"files-over", idUser, "4", "5", false},
			{"size-over", idUser, "3", "6", false},
			{"exact-caps", idUser, "3", "5", true},
			{"admin-zero", idAdmin, "0", "0", true},
			{"admin-over", idAdmin, "4", "6", true},
			{"superadmin-zero", idSuperAdmin, "0", "0", true},
			{"superadmin-over", idSuperAdmin, "4", "6", true},
		}},
		{"files-uncapped", 0, 5, []creationCase{
			{"limited-size-omitted", idUser, "9", "", false},
			{"limited-size-zero", idUser, "9", "0", false},
			{"limited-size-over", idUser, "9", "6", false},
			{"uncapped-files-omitted", idUser, "", "5", true},
			{"uncapped-files-zero", idUser, "0", "5", true},
			{"uncapped-files-positive", idUser, "9", "5", true},
		}},
		{"size-uncapped", 3, 0, []creationCase{
			{"limited-files-omitted", idUser, "", "9", false},
			{"limited-files-zero", idUser, "0", "9", false},
			{"limited-files-over", idUser, "4", "9", false},
			{"uncapped-size-omitted", idUser, "3", "", true},
			{"uncapped-size-zero", idUser, "3", "0", true},
			{"uncapped-size-positive", idUser, "3", "9", true},
		}},
		{"both-uncapped", 0, 0, []creationCase{
			{"omitted", idUser, "", "", true},
			{"zero", idUser, "0", "0", true},
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			originalConfig := *configuration.Get()
			// Setenv cleanups run first, so Load restores the original cached
			// environment without reconnecting the provider-global fixture.
			t.Cleanup(func() {
				configuration.Load()
				*configuration.Get() = originalConfig
			})
			t.Setenv("GOKAPI_MAX_FILES_GUESTUPLOAD", strconv.Itoa(scenario.files))
			t.Setenv("GOKAPI_MAX_SIZE_GUESTUPLOAD", strconv.Itoa(scenario.size))
			configuration.Load()
			for _, tc := range scenario.cases {
				t.Run(tc.name, func(t *testing.T) {
					beforeRequests, beforeKeys := requestIDs(), keyIDs()
					t.Cleanup(func() {
						for _, request := range database.GetAllFileRequests() {
							if _, existed := beforeRequests[request.Id]; !existed {
								database.DeleteFileRequest(request)
							}
						}
						for id := range database.GetAllApiKeys() {
							if _, existed := beforeKeys[id]; !existed {
								database.DeleteApiKey(id)
							}
						}
					})
					r := httptest.NewRequest(http.MethodPost, "/api/uploadrequest/save", nil)
					r.Header.Set("apikey", keys[tc.owner])
					if tc.files != "" {
						r.Header.Set("maxfiles", tc.files)
					}
					if tc.size != "" {
						r.Header.Set("maxsize", tc.size)
					}
					w := httptest.NewRecorder()
					Process(w, r)
					if !tc.allowed {
						var result struct{ ErrorCode int }
						if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusBadRequest || result.ErrorCode != errorcodes.AdminOnly {
							t.Fatalf("expected guest-cap rejection: status=%d body=%s", w.Code, w.Body.String())
						}
						if !maps.Equal(beforeRequests, requestIDs()) || !maps.Equal(beforeKeys, keyIDs()) {
							t.Fatal("rejected creation persisted a request or API key")
						}
						return
					}
					var request models.FileRequest
					if err := json.Unmarshal(w.Body.Bytes(), &request); err != nil || w.Code != http.StatusOK || request.Id == "" || request.ApiKey == "" {
						t.Fatalf("expected creation success: status=%d body=%s", w.Code, w.Body.String())
					}
					wantFiles, _ := strconv.Atoi(tc.files)
					wantSize, _ := strconv.Atoi(tc.size)
					stored, found := database.GetFileRequest(request.Id)
					if !found || stored.UserId != tc.owner || stored.MaxFiles != wantFiles || stored.MaxSize != wantSize || stored.AllowedDownloads != 1 {
						t.Fatal("creation did not preserve owner, limits, or download default")
					}
					key, found := database.GetApiKey(request.ApiKey)
					if !found || key.UserId != tc.owner || key.UploadRequestId != request.Id || stored.ApiKey != key.Id {
						t.Fatal("creation did not bind its scoped upload credential")
					}
					afterRequests, afterKeys := requestIDs(), keyIDs()
					delete(afterRequests, request.Id)
					delete(afterKeys, request.ApiKey)
					if !maps.Equal(beforeRequests, afterRequests) || !maps.Equal(beforeKeys, afterKeys) {
						t.Fatal("creation changed unrelated requests or API keys")
					}
				})
			}
		})
	}
}
