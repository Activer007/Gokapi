//go:build test && !integration

package webserver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/storage/presign"
	"github.com/Activer007/Gokapi/internal/test/securityfixture"
	"github.com/Activer007/Gokapi/internal/webserver/api"
	"github.com/Activer007/Gokapi/internal/webserver/authentication"
)

type presignTestStore struct{}

func (presignTestStore) SaveUser(v models.User, fresh bool)      { database.SaveUser(v, fresh) }
func (presignTestStore) SaveApiKey(v models.ApiKey)              { database.SaveApiKey(v) }
func (presignTestStore) SaveSession(id string, v models.Session) { database.SaveSession(id, v) }
func (presignTestStore) SaveMetaData(v models.File)              { database.SaveMetaData(v) }
func (presignTestStore) SaveFileRequest(v models.FileRequest)    { database.SaveFileRequest(v) }

func issueTestPresign(t *testing.T, key string, ids []string) string {
	t.Helper()
	path := "/api/files/download/" + ids[0]
	if len(ids) > 1 {
		path = "/api/files/downloadzip"
	}
	r := httptest.NewRequest("POST", path, nil)
	r.Header.Set("apikey", key)
	r.Header.Set("presignUrl", "true")
	r.Header.Set("ids", strings.Join(ids, ","))
	w := httptest.NewRecorder()
	api.Process(w, r)
	if w.Code != 200 {
		t.Fatalf("presign issuance status=%d", w.Code)
	}
	var result struct {
		URL string `json:"downloadUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(result.URL)
	if err != nil || u.Query().Get("key") == "" {
		t.Fatal("missing capability")
	}
	id := u.Query().Get("key")
	t.Cleanup(func() { presign.Delete(id) })
	return id
}

// The API issues the capability normally. Consumption starts at the handler's
// authenticated-context boundary; caller identity is never taken from URL data.
// Anonymous middleware rejection is tested separately. This is not a browser test.
func TestSEC021PresignConsumption(t *testing.T) {
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "presign.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "presign_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, tc := range []struct {
				name  string
				allow bool
			}{
				{"owner", true}, {"authorized-admin", true}, {"zip", true}, {"other-user", false},
				{"revoked-key", false}, {"expired-key", false}, {"key-permission", false}, {"request-key", false},
				{"deleted-issuer", false}, {"issuer-delegation", false}, {"consumer-delegation", false},
				{"changed-owner", false}, {"zip-member-owner", false}, {"expired-grant", false}, {"pending-file", false},
				{"temporary", true}, {"revoked-temporary", false}, {"deleted-consumer", false},
				{"missing-context", false}, {"unbound-grant", false}, {"concurrent", true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := securityfixture.New(t, "")
					s.Seed(presignTestStore{})
					oldDir := configuration.Get().DataDir
					configuration.Get().DataDir = s.DataDir
					t.Cleanup(func() { configuration.Get().DataDir = oldDir })
					issuer, consumer := s.Users["owner"], s.Users["owner"]
					key := s.Keys["owner"]
					if tc.name == "temporary" || tc.name == "revoked-temporary" {
						key = s.Keys["temporary"]
					}
					ids := []string{s.Files["own"].Id}
					if tc.name == "zip" || tc.name == "zip-member-owner" {
						ids = append(ids, s.Files["protected"].Id)
					}
					if tc.name == "authorized-admin" || tc.name == "consumer-delegation" || tc.name == "deleted-consumer" {
						consumer = s.Users["admin"]
					}
					if tc.name == "issuer-delegation" {
						issuer = s.Users["admin"]
						key = s.Keys["admin"]
					}
					grant := issueTestPresign(t, key.Id, ids)
					switch tc.name {
					case "other-user":
						consumer = s.Users["other"]
					case "revoked-key", "revoked-temporary":
						database.DeleteApiKey(key.Id)
					case "expired-key":
						key.Expiry = time.Now().Add(-time.Second).Unix()
						database.SaveApiKey(key)
					case "key-permission":
						key.RemovePermission(models.ApiPermDownload)
						database.SaveApiKey(key)
					case "request-key":
						key.UploadRequestId = s.Request.Id
						database.SaveApiKey(key)
					case "deleted-issuer":
						database.DeleteUser(issuer.Id)
						consumer = s.Users["admin"]
					case "issuer-delegation":
						issuer.RemovePermission(models.UserPermListOtherUploads)
						database.SaveUser(issuer, false)
					case "consumer-delegation":
						consumer.RemovePermission(models.UserPermListOtherUploads)
						database.SaveUser(consumer, false)
					case "changed-owner":
						f := s.Files["own"]
						f.UserId = s.Users["other"].Id
						database.SaveMetaData(f)
					case "zip-member-owner":
						f := s.Files["protected"]
						f.UserId = s.Users["other"].Id
						database.SaveMetaData(f)
					case "expired-grant":
						p, _ := presign.Get(grant)
						p.Expiry = time.Now().Add(-time.Second).Unix()
						presign.Save(p)
					case "pending-file":
						f := s.Files["own"]
						f.PendingDeletion = time.Now().Add(time.Minute).Unix()
						database.SaveMetaData(f)
					case "deleted-consumer":
						database.DeleteUser(consumer.Id)
					case "unbound-grant":
						p, _ := presign.Get(grant)
						presign.Save(models.Presign{Id: p.Id, FileIds: p.FileIds, Expiry: p.Expiry, Filename: p.Filename})
					}
					r := httptest.NewRequest("GET", "/downloadPresigned?key="+grant, nil)
					// Preserve the originally authenticated snapshot: the handler must
					// reload current authority rather than trust old context grants.
					contextUser := s.Users["owner"]
					if consumer.Id == s.Users["admin"].Id {
						contextUser = s.Users["admin"]
					} else if consumer.Id == s.Users["other"].Id {
						contextUser = s.Users["other"]
					}
					w := httptest.NewRecorder()
					if tc.name == "concurrent" {
						var wg sync.WaitGroup
						var success atomic.Int32
						start := make(chan struct{})
						for i := 0; i < 16; i++ {
							wg.Add(1)
							go func() {
								defer wg.Done()
								<-start
								response := httptest.NewRecorder()
								downloadPresigned(response, authentication.SetUserInRequest(r.Clone(r.Context()), contextUser))
								if response.Code == 200 {
									success.Add(1)
									if !bytes.Equal(response.Body.Bytes(), []byte("synthetic file own")) {
										t.Error("concurrent bytes differ")
									}
								} else if response.Code != 400 {
									t.Errorf("unexpected concurrent status=%d", response.Code)
								}
							}()
						}
						close(start)
						wg.Wait()
						if success.Load() != 1 {
							t.Fatalf("successful consumers=%d want=1", success.Load())
						}
						return
					}
					if tc.name == "missing-context" {
						downloadPresigned(w, r)
					} else {
						downloadPresigned(w, authentication.SetUserInRequest(r, contextUser))
					}
					if tc.allow {
						if w.Code != 200 {
							t.Fatalf("valid consumption status=%d", w.Code)
						}
						if tc.name == "zip" {
							z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
							if err != nil || len(z.File) != 2 {
								t.Fatal("invalid ZIP output")
							}
						} else if !bytes.Equal(w.Body.Bytes(), []byte("synthetic file own")) {
							t.Fatal("download bytes differ")
						}
						second := httptest.NewRecorder()
						downloadPresigned(second, authentication.SetUserInRequest(r, contextUser))
						if second.Code == 200 {
							t.Fatal("capability replay accepted")
						}
					} else if w.Code == 200 || bytes.Contains(w.Body.Bytes(), []byte("synthetic file")) || bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
						t.Fatalf("disallowed consumption returned content: status=%d", w.Code)
					}
					if tc.name == "other-user" {
						ownerResponse := httptest.NewRecorder()
						downloadPresigned(ownerResponse, authentication.SetUserInRequest(r, s.Users["owner"]))
						if ownerResponse.Code != 200 {
							t.Fatal("unauthorized consumer invalidated owner's capability")
						}
					}
				})
			}
		})
	}
}

func TestSEC021PresignAnonymous(t *testing.T) {
	r := httptest.NewRequest("GET", "/downloadPresigned?key=synthetic-invalid", nil)
	w := httptest.NewRecorder()
	requireLogin(downloadPresigned, false, false)(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous presign status=%d", w.Code)
	}
}
