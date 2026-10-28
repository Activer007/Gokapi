//go:build test

package securityfixture_test

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/presign"
	"github.com/forceu/gokapi/internal/test/securityfixture"
	"github.com/forceu/gokapi/internal/test/testconfiguration"
	"github.com/forceu/gokapi/internal/webserver/api"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
	"github.com/forceu/gokapi/internal/webserver/authentication/tokengeneration"
	"github.com/forceu/gokapi/internal/webserver/ratelimiter"
)

const matrixAllKeyPermissions = models.ApiPermDefault | models.ApiPermDownload | models.ApiPermReplace | models.ApiPermApiMod | models.ApiPermManageUsers | models.ApiPermManageLogs | models.ApiPermManageFileRequests

func matrixConfig(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("GOKAPI_CONFIG_DIR", "test")
	t.Setenv("GOKAPI_DATA_DIR", "test/data")
	testconfiguration.Create(false)
	configuration.Load()
	ratelimiter.SetUnitTestMode(true)
	t.Cleanup(func() { ratelimiter.SetUnitTestMode(false) })
}

func matrixScene(t *testing.T, backend string) securityfixture.Scene {
	t.Helper()
	cfg := models.DbConnection{Type: 0, HostUrl: filepath.Join(t.TempDir(), "matrix.sqlite")}
	if backend == "redis" {
		cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "m2_"}
	}
	database.Connect(cfg)
	t.Cleanup(database.Close)
	s := securityfixture.New(t, "")
	s.Users["super"] = models.User{Id: 7000, Name: "synthetic-super", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	for _, name := range []string{"owner", "other"} {
		u := s.Users[name]
		u.GrantPermission(models.UserPermReplaceUploads)
		s.Users[name] = u
	}
	for name, u := range s.Users {
		perms := models.ApiPermDefault | models.ApiPermDownload | models.ApiPermReplace | models.ApiPermApiMod
		if u.IsAdmin() {
			perms = matrixAllKeyPermissions
		}
		s.Keys[name] = models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: u.Id, Permissions: perms}
	}
	s.Seed(globalStore{})
	oldDir := configuration.Get().DataDir
	configuration.Get().DataDir = s.DataDir
	t.Cleanup(func() { configuration.Get().DataDir = oldDir })
	return s
}

func matrixCall(key, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api"+path, nil)
	if key != "" {
		r.Header.Set("apikey", key)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	api.Process(w, r)
	return w
}

// Uses real API parsing/authorization and both stores. No global storage GC is
// started: delete is a negative-only case here; its lifecycle has separate tests.
func TestSEC020ManagementMatrix(t *testing.T) {
	matrixConfig(t)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := matrixScene(t, backend)
			for _, actor := range []string{"super", "admin", "owner", "other", "anonymous", "request", "expired", "revoked", "deleted"} {
				kinds := []string{"permanent", "temporary", "session-token"}
				if actor == "anonymous" || actor == "request" || actor == "expired" || actor == "revoked" || actor == "deleted" {
					kinds = []string{"credential"}
				}
				for _, kind := range kinds {
					t.Run(actor+"/"+kind, func(t *testing.T) {
						u := s.Users[actor]
						key := s.Keys[actor]
						valid := actor == "super" || actor == "admin" || actor == "owner" || actor == "other"
						if !valid {
							key = s.Keys["owner"]
							key.Id, key.PublicId = rand.Text(), rand.Text()
							key.Permissions = matrixAllKeyPermissions // type/liveness must reject, not missing bits
							switch actor {
							case "anonymous":
								key.Id = ""
							case "request":
								key.UploadRequestId = s.Request.Id
							case "expired":
								key.Expiry = time.Now().Add(-time.Minute).Unix()
							case "deleted":
								key.UserId = 7999
							}
							database.SaveApiKey(key)
							if actor == "revoked" {
								database.DeleteApiKey(key.Id)
							}
						} else if kind == "temporary" {
							id, _, err := tokengeneration.Generate(u, key.Permissions)
							if err != nil {
								t.Fatal("valid token fixture rejected")
							}
							key, _ = database.GetApiKey(id)
						} else if kind == "session-token" {
							id := rand.Text()
							database.SaveSession(id, models.Session{UserId: u.Id, RenewAt: time.Now().Add(time.Hour).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()})
							r := httptest.NewRequest("GET", "/auth/token", nil)
							r.AddCookie(&http.Cookie{Name: "session_token", Value: id})
							current, ok := sessionmanager.IsValidSession(httptest.NewRecorder(), r, false, 1)
							if !ok || current.Id != u.Id {
								t.Fatal("session fixture rejected")
							}
							generated, _, err := tokengeneration.Generate(current, key.Permissions)
							if err != nil {
								t.Fatal("session token fixture rejected")
							}
							key, _ = database.GetApiKey(generated)
						}
						defer database.DeleteApiKey(key.Id)
						for _, action := range []string{"list", "metadata", "download", "zip", "presign", "modify", "replace", "copy", "restore", "delete", "users", "logs"} {
							t.Run(action, func(t *testing.T) {
								target, source := s.Files["other"], s.Files["own"]
								// Fresh metadata for each action. Source belongs to caller for positive replacement.
								if valid {
									source.UserId = u.Id
								}
								if action == "restore" {
									target.PendingDeletion = time.Now().Add(time.Minute).Unix()
								}
								database.SaveMetaData(target)
								database.SaveMetaData(source)
								before := database.GetAllMetadata()
								blobBefore, err := os.ReadFile(filepath.Join(s.DataDir, target.SHA1))
								if err != nil {
									t.Fatal(err)
								}
								allowed := valid && (actor == "super" || actor == "admin" || actor == "other")
								if action == "list" {
									allowed = valid
								}
								if action == "users" || action == "logs" {
									allowed = valid && u.IsAdmin()
								}
								if action == "delete" && allowed {
									t.Skip("positive async deletion belongs to isolated lifecycle tests")
								}
								path := "/files/" + action
								headers := map[string]string{"id": target.Id}
								switch action {
								case "metadata":
									path = "/files/list/" + target.Id
								case "download", "presign":
									path = "/files/download/" + target.Id
									headers["presignUrl"] = strconv.FormatBool(action == "presign")
								case "zip":
									path = "/files/downloadzip"
									headers["ids"] = target.Id
								case "modify":
									headers["allowedDownloads"] = "23"
								case "replace":
									headers["idNewContent"] = source.Id
								case "copy":
									path = "/files/duplicate"
								case "users":
									path = "/user/create"
									headers["username"] = "synthetic-" + rand.Text()
								case "logs":
									path = "/logs/get"
								}
								w := matrixCall(key.Id, path, headers)
								want := http.StatusUnauthorized
								if allowed {
									want = http.StatusOK
								}
								if w.Code != want {
									t.Fatalf("status=%d want=%d", w.Code, want)
								}
								if !allowed {
									if !reflect.DeepEqual(before, database.GetAllMetadata()) {
										t.Fatal("denied action changed metadata")
									}
									after, err := os.ReadFile(filepath.Join(s.DataDir, target.SHA1))
									if err != nil || !bytes.Equal(blobBefore, after) {
										t.Fatal("denied action changed content")
									}
									return
								}
								switch action {
								case "list":
									var files []models.FileApiOutput
									if err := json.Unmarshal(w.Body.Bytes(), &files); err != nil {
										t.Fatal(err)
									}
									found := false
									for _, f := range files {
										if f.Id == target.Id {
											found = true
										}
									}
									if found != (actor != "owner") {
										t.Fatal("list leaked or omitted target")
									}
								case "download":
									if !bytes.Equal(w.Body.Bytes(), blobBefore) {
										t.Fatal("download bytes differ")
									}
								case "zip":
									z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
									if err != nil || len(z.File) != 1 {
										t.Fatal("invalid zip")
									}
									f, err := z.File[0].Open()
									if err != nil {
										t.Fatal(err)
									}
									b, err := io.ReadAll(f)
									f.Close()
									if err != nil || !bytes.Equal(b, blobBefore) {
										t.Fatal("zip bytes differ")
									}
								case "presign":
									var out struct {
										URL string `json:"downloadUrl"`
									}
									if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
										t.Fatal(err)
									}
									parsed, err := url.Parse(out.URL)
									if err != nil {
										t.Fatal("invalid presign response")
									}
									id := parsed.Query().Get("key")
									p, ok := presign.Get(id)
									defer presign.Delete(id)
									if !ok || len(p.FileIds) != 1 || p.FileIds[0] != target.Id {
										t.Fatal("presign not scoped to requested file")
									}
								case "modify":
									f, _ := database.GetMetaDataById(target.Id)
									if f.DownloadsRemaining != 23 {
										t.Fatal("edit not applied")
									}
								case "replace":
									f, _ := database.GetMetaDataById(target.Id)
									if f.SHA1 != source.SHA1 || f.UserId != target.UserId {
										t.Fatal("replacement content or owner incorrect")
									}
								case "copy":
									if len(database.GetAllMetadata()) != len(before)+1 {
										t.Fatal("copy not created")
									}
								case "restore":
									f, _ := database.GetMetaDataById(target.Id)
									if f.PendingDeletion != 0 {
										t.Fatal("restore not applied")
									}
								}
							})
						}
					})
				}
			}
		})
	}
}

// Historical replacement regression: target authority, source visibility and
// optional source deletion are independent. Every negative leaves both records
// and blobs intact, including when two owners reference the same blob.
func TestSEC020ReplaceBoundaries(t *testing.T) {
	matrixConfig(t)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := matrixScene(t, backend)
			cases := []struct {
				name, target, source    string
				perms                   models.UserPermission
				keyDelete, deleteSource bool
				want                    int
			}{
				{"own-pair", "own", "protected", models.UserPermReplaceUploads, false, false, 200},
				{"other-target", "other", "own", models.UserPermReplaceUploads, false, false, 401},
				{"other-source", "own", "other", models.UserPermReplaceUploads, false, false, 401},
				{"same-blob-other-owner", "own", "dedup", models.UserPermReplaceUploads, false, false, 401},
				{"target-delegated", "other", "own", models.UserPermReplaceUploads | models.UserPermReplaceOtherUploads, false, false, 200},
				{"source-visible", "own", "other", models.UserPermReplaceUploads | models.UserPermListOtherUploads, false, false, 200},
				{"delete-needs-key-bit", "own", "other", models.UserPermReplaceUploads | models.UserPermListOtherUploads | models.UserPermDeleteOtherUploads, false, true, 401},
				{"delete-needs-owner-authority", "own", "other", models.UserPermReplaceUploads | models.UserPermListOtherUploads, true, true, 401},
				{"own-delete-needs-key-bit", "own", "protected", models.UserPermReplaceUploads, false, true, 401},
				{"delete-does-not-grant-read", "own", "other", models.UserPermReplaceUploads | models.UserPermDeleteOtherUploads, true, true, 401},
				{"read-delete-do-not-grant-target", "other", "own", models.UserPermReplaceUploads | models.UserPermListOtherUploads | models.UserPermDeleteOtherUploads, true, true, 401},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					for _, f := range s.Files {
						database.SaveMetaData(f)
					}
					u := s.Users["owner"]
					u.Permissions = tc.perms
					database.SaveUser(u, false)
					key := s.Keys["owner"]
					key.Permissions = models.ApiPermReplace
					if tc.keyDelete {
						key.Permissions |= models.ApiPermDelete
					}
					database.SaveApiKey(key)
					before := database.GetAllMetadata()
					w := matrixCall(key.Id, "/files/replace", map[string]string{"id": s.Files[tc.target].Id, "idNewContent": s.Files[tc.source].Id, "deleteNewFile": strconv.FormatBool(tc.deleteSource)})
					if w.Code != tc.want {
						t.Fatalf("status=%d want=%d", w.Code, tc.want)
					}
					if tc.want == 401 && !reflect.DeepEqual(before, database.GetAllMetadata()) {
						t.Fatal("denied replace changed metadata")
					}
					for _, name := range []string{tc.target, tc.source} {
						f := s.Files[name]
						b, err := os.ReadFile(filepath.Join(s.DataDir, f.SHA1))
						if err != nil || len(b) == 0 {
							t.Fatal("replacement removed a fixture blob")
						}
					}
				})
			}
		})
	}
}

func TestSEC020MissingKeyPermission(t *testing.T) {
	matrixConfig(t)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := matrixScene(t, backend)
			for _, tc := range []struct {
				path       string
				permission models.ApiPermission
			}{
				{"/files/list", models.ApiPermView}, {"/files/downloadzip", models.ApiPermDownload},
				{"/files/modify", models.ApiPermEdit}, {"/files/replace", models.ApiPermReplace},
				{"/files/duplicate", models.ApiPermUpload}, {"/files/restore", models.ApiPermDelete}, {"/files/delete", models.ApiPermDelete},
				{"/user/create", models.ApiPermManageUsers}, {"/logs/get", models.ApiPermManageLogs},
			} {
				t.Run(tc.path, func(t *testing.T) {
					key := s.Keys["super"]
					key.Permissions = matrixAllKeyPermissions &^ tc.permission
					database.SaveApiKey(key)
					if w := matrixCall(key.Id, tc.path, nil); w.Code != 401 {
						t.Fatalf("super-admin key without action bit: status=%d", w.Code)
					}
				})
			}
		})
	}
}

// Simulate a stale key record left by an interrupted permission update. This is
// a defensive state invariant, not proof of an externally exploitable sequence.
func TestSEC020CurrentPermissionIntersection(t *testing.T) {
	matrixConfig(t)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := matrixScene(t, backend)
			u := s.Users["owner"]
			u.Permissions = models.UserPermissionNone
			database.SaveUser(u, false)
			for _, temporary := range []bool{false, true} {
				key := s.Keys["owner"]
				key.Permissions = matrixAllKeyPermissions
				key.IsSystemKey = temporary
				if temporary {
					key.Expiry = time.Now().Add(time.Minute).Unix()
				}
				database.SaveApiKey(key)
				for _, tc := range []struct {
					path    string
					headers map[string]string
				}{
					{"/logs/get", nil}, {"/user/create", map[string]string{"username": "synthetic-" + rand.Text()}},
					{"/uploadrequest/list", nil}, {"/files/replace", map[string]string{"id": s.Files["own"].Id, "idNewContent": s.Files["protected"].Id}},
				} {
					t.Run(strconv.FormatBool(temporary)+tc.path, func(t *testing.T) {
						before := database.GetAllMetadata()
						w := matrixCall(key.Id, tc.path, tc.headers)
						if w.Code != 401 {
							t.Fatalf("stale key bypassed current user permission: status=%d", w.Code)
						}
						if !reflect.DeepEqual(before, database.GetAllMetadata()) {
							t.Fatal("denied stale key changed metadata")
						}
					})
				}
			}
		})
	}
}
