//go:build test

package api

import (
	"crypto/rand"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"net/http/httptest"
	"testing"
)

func TestSEC020CredentialTypeEveryRoute(t *testing.T) {
	user := models.User{Id: 9020, Name: "synthetic-credential-boundary", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll}
	database.SaveUser(user, false)
	t.Cleanup(func() { database.DeleteUser(user.Id) })
	key := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: user.Id, Permissions: 1023}
	t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	for _, route := range routes {
		t.Run(route.Url, func(t *testing.T) {
			key.UploadRequestId = ""
			if !route.IsFileRequestApi {
				key.UploadRequestId = "synthetic-request"
			}
			database.SaveApiKey(key)
			path := "/api" + route.Url
			if route.HasWildcard {
				path += "synthetic-file"
			}
			r := httptest.NewRequest("POST", path, nil)
			r.Header.Set("apikey", key.Id)
			w := httptest.NewRecorder()
			Process(w, r)
			if w.Code != 401 {
				t.Fatalf("wrong credential type entered route, status=%d", w.Code)
			}
		})
	}
}
