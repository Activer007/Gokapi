//go:build test

package securityfixture_test

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/configuration/database/dbabstraction"
	"github.com/Activer007/Gokapi/internal/configuration/database/dbcache"
	redisdb "github.com/Activer007/Gokapi/internal/configuration/database/provider/redis"
	"github.com/Activer007/Gokapi/internal/configuration/database/provider/sqlite"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/webserver/authentication/sessionmanager"
	"github.com/Activer007/Gokapi/internal/webserver/authentication/tokengeneration"
)

// Usage telemetry is not an authority to create records or restore old fields.
// A held snapshot models an update already queued when an administrator edits
// or removes the record. This provider contract is independent of HTTP timing.
func TestSEC021UsageUpdateContract(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			var db dbabstraction.Database
			var err error
			var mini *miniredis.Miniredis
			if backend == "redis" {
				mini = miniredis.RunT(t)
				db, err = redisdb.New(models.DbConnection{HostUrl: mini.Addr(), RedisPrefix: "sec021_"})
			} else {
				db, err = sqlite.New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "usage.sqlite")})
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(db.Close)
			t.Run("current-fields-and-expiry", func(t *testing.T) {
				old := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: 1, Permissions: models.ApiPermDownload, Expiry: time.Now().Add(time.Hour).Unix(), IsSystemKey: true}
				db.SaveApiKey(old)
				current := old
				current.Permissions = models.ApiPermNone
				current.FriendlyName = "synthetic-renamed"
				current.Expiry = time.Now().Add(time.Minute).Unix()
				db.SaveApiKey(current)
				var ttl time.Duration
				if mini != nil {
					ttl = mini.TTL("sec021_apikey:" + old.Id)
				}
				old.LastUsed = time.Now().Unix()
				db.UpdateTimeApiKey(old)
				got, ok := db.GetApiKey(old.Id)
				current.LastUsed = old.LastUsed
				if !ok || got != current {
					t.Error("usage update changed non-usage fields")
				}
				if mini != nil && mini.TTL("sec021_apikey:"+old.Id) != ttl {
					t.Error("usage update changed TTL")
				}
			})
			t.Run("removed-key", func(t *testing.T) {
				key := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: 1, Permissions: models.ApiPermView}
				db.SaveApiKey(key)
				db.DeleteApiKey(key.Id)
				key.LastUsed = time.Now().Unix()
				db.UpdateTimeApiKey(key)
				if _, ok := db.GetApiKey(key.Id); ok {
					t.Fatal("usage update recreated removed key")
				}
			})
			t.Run("removed-user", func(t *testing.T) {
				u := models.User{Id: 21, Name: "synthetic-user", UserLevel: models.UserLevelUser}
				db.SaveUser(u, false)
				db.DeleteUser(u.Id)
				db.UpdateUserLastOnline(u.Id)
				if _, ok := db.GetUser(u.Id); ok {
					t.Fatal("online update recreated removed user")
				}
			})
			t.Run("current-user", func(t *testing.T) {
				u := models.User{Id: 22, Name: "synthetic-current", UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads}
				db.SaveUser(u, false)
				db.UpdateUserLastOnline(u.Id)
				got, ok := db.GetUser(u.Id)
				if !ok || got.LastOnline == 0 {
					t.Fatal("online time not updated")
				}
				u.LastOnline = got.LastOnline
				if got != u {
					t.Fatal("online update changed identity or permission")
				}
			})
			t.Run("concurrent-remove-and-usage", func(t *testing.T) {
				for i := 0; i < 20; i++ {
					key := models.ApiKey{Id: rand.Text(), UserId: 1, Permissions: models.ApiPermView, LastUsed: time.Now().Unix()}
					db.SaveApiKey(key)
					start := make(chan struct{})
					var wg sync.WaitGroup
					wg.Add(2)
					go func() { defer wg.Done(); <-start; db.UpdateTimeApiKey(key) }()
					go func() { defer wg.Done(); <-start; db.DeleteApiKey(key.Id) }()
					close(start)
					wg.Wait()
					if _, ok := db.GetApiKey(key.Id); ok {
						t.Fatal("concurrent usage retained removed key")
					}
				}
			})
		})
	}
}

// Real API mutations followed by fresh requests, rather than pre-seeded invalid
// credentials. Deleting a user transfers synthetic files and never starts GC.
func TestSEC021RevocationAPI(t *testing.T) {
	matrixConfig(t)
	for _, backend := range []string{"sqlite", "redis"} {
		for _, mutation := range []string{"demote", "remove-log-permission", "delete-user", "delete-key"} {
			t.Run(backend+"/"+mutation, func(t *testing.T) {
				s := matrixScene(t, backend)
				u := s.Users["admin"]
				key := s.Keys["admin"]
				token, _, err := tokengeneration.Generate(u, matrixAllKeyPermissions)
				if err != nil {
					t.Fatal(err)
				}
				keys := []string{key.Id, token}
				for _, id := range keys {
					if w := matrixCall(id, "/logs/get", nil); w.Code != 200 {
						t.Fatal("positive credential rejected")
					}
				}
				session := rand.Text()
				database.SaveSession(session, models.Session{UserId: u.Id, RenewAt: time.Now().Add(time.Hour).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()})
				path := "/user/changeRank"
				headers := map[string]string{"userid": strconv.Itoa(u.Id), "newRank": "user"}
				switch mutation {
				case "remove-log-permission":
					path = "/user/modify"
					headers = map[string]string{"userid": strconv.Itoa(u.Id), "userpermission": "PERM_LOGS", "permissionModifier": "revoke"}
				case "delete-user":
					path = "/user/delete"
					headers = map[string]string{"userid": strconv.Itoa(u.Id), "deleteFiles": "false"}
				case "delete-key":
					path = "/auth/delete"
					headers = map[string]string{"targetKey": key.Id}
				}
				if w := matrixCall(s.Keys["super"].Id, path, headers); w.Code != 200 {
					t.Fatalf("mutation %s status=%d", mutation, w.Code)
				}
				before := database.GetAllMetadata()
				for _, id := range keys {
					want := 401
					if mutation == "delete-key" && id == token {
						want = 200
					}
					if w := matrixCall(id, "/logs/get", nil); w.Code != want {
						t.Fatalf("post-mutation access status=%d want=%d", w.Code, want)
					}
					if mutation == "demote" || mutation == "delete-user" {
						for _, route := range []string{"/user/create", "/uploadrequest/list", "/files/replace"} {
							if w := matrixCall(id, route, nil); w.Code != 401 {
								t.Fatalf("revoked management action %s status=%d", route, w.Code)
							}
						}
					}
					if mutation == "demote" || mutation == "delete-user" || (mutation == "delete-key" && id == key.Id) {
						target := s.Files["other"].Id
						for _, action := range []struct {
							path    string
							headers map[string]string
						}{
							{"/files/list/" + target, nil}, {"/files/download/" + target, nil},
							{"/files/download/" + target, map[string]string{"presignUrl": "true"}},
							{"/files/downloadzip", map[string]string{"ids": target}},
							{"/files/modify", map[string]string{"id": target, "allowedDownloads": "2"}},
							{"/files/replace", map[string]string{"id": target, "idNewContent": s.Files["own"].Id}},
							{"/files/duplicate", map[string]string{"id": target}},
							{"/files/restore", map[string]string{"id": target}},
							{"/files/delete", map[string]string{"id": target}},
						} {
							if w := matrixCall(id, action.path, action.headers); w.Code != 401 {
								t.Fatalf("post-revocation file action %s status=%d", action.path, w.Code)
							}
						}
						w := matrixCall(id, "/files/list", nil)
						if mutation == "demote" {
							if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(target)) {
								t.Fatal("demoted list retained cross-owner visibility")
							}
						} else if w.Code != 401 {
							t.Fatal("revoked identity listed files")
						}
					}
				}
				if !reflect.DeepEqual(before, database.GetAllMetadata()) {
					t.Fatal("denied operations changed files")
				}
				r := httptest.NewRequest("GET", "/auth/token", nil)
				r.AddCookie(&http.Cookie{Name: "session_token", Value: session})
				// Keep unrelated asynchronous online telemetry out of the global DB
				// fixture lifetime. Its provider behavior is tested separately above.
				var live models.User
				var ok bool
				synctest.Test(t, func(t *testing.T) {
					dbcache.RequireSaveUserOnline(u.Id)
					live, ok = sessionmanager.IsValidSession(httptest.NewRecorder(), r, false, 1)
					synctest.Wait()
				})
				if mutation == "delete-user" {
					if ok {
						t.Fatal("deleted user's session accepted")
					}
				} else {
					if !ok {
						t.Fatal("unrelated session invalidated")
					}
					if mutation != "delete-key" {
						if _, _, err := tokengeneration.Generate(live, models.ApiPermManageLogs); err == nil {
							t.Fatal("session retained removed log permission")
						}
					}
				}
			})
		}
	}
}

func TestSEC021MixedZIP(t *testing.T) {
	matrixConfig(t)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := matrixScene(t, backend)
			key := s.Keys["owner"].Id
			own, other := s.Files["own"].Id, s.Files["other"].Id
			if w := matrixCall(key, "/files/download/"+own, nil); w.Code != 200 {
				t.Fatal("own download positive rejected")
			}
			before := database.GetAllMetadata()
			for _, ids := range []string{own + "," + other, other + "," + own} {
				for _, presign := range []string{"false", "true"} {
					w := matrixCall(key, "/files/downloadzip", map[string]string{"ids": ids, "presignUrl": presign})
					if w.Code != 401 {
						t.Fatalf("mixed ZIP accepted or failed before authorization: status=%d", w.Code)
					}
					if bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) || bytes.Contains(w.Body.Bytes(), []byte("downloadUrl")) {
						t.Fatal("denied ZIP emitted content or capability")
					}
				}
			}
			if !reflect.DeepEqual(before, database.GetAllMetadata()) {
				t.Fatal("denied ZIP changed metadata")
			}
		})
	}
}
