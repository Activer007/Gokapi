//go:build test && !integration

package webserver

import (
	"bytes"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/test/securityfixture"
	"github.com/Activer007/Gokapi/internal/webserver/authentication/downloadPasswordToken"
)

// Public sharing is an explicit read capability, independent of ownership.
// Test handlers with real stores/blobs, without starting a storage GC worker.
func TestSEC020PublicDownloadMatrix(t *testing.T) {
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	const password = "synthetic-download-password"
	hash := configuration.HashPassword(password, false, "")
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "public.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "public_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, endpoint := range []string{"download", "filename", "hotlink"} {
				for _, state := range []string{"public", "other-owner", "expired", "exhausted", "pending", "request", "missing", "password-missing", "password-wrong-file", "password-revoked", "password-valid", "request-key", "session-id", "api-key"} {
					// Password-protected files cannot acquire hotlinks through the
					// supported create/edit flow; that lifecycle is a separate gate.
					if endpoint == "hotlink" && (strings.HasPrefix(state, "password-") || state == "request-key" || state == "session-id" || state == "api-key") {
						continue
					}
					t.Run(endpoint+"/"+state, func(t *testing.T) {
						s := securityfixture.New(t, hash)
						s.Seed(presignTestStore{})
						oldDir := configuration.Get().DataDir
						configuration.Get().DataDir = s.DataDir
						t.Cleanup(func() { configuration.Get().DataDir = oldDir })
						file := s.Files["own"]
						allow := state == "public" || state == "other-owner" || state == "password-valid"
						switch state {
						case "other-owner":
							file = s.Files["other"]
						case "expired":
							file.UnlimitedTime = false
							file.ExpireAt = time.Now().Add(-time.Minute).Unix()
						case "exhausted":
							file.UnlimitedDownloads = false
							file.DownloadsRemaining = 0
						case "pending":
							file.PendingDeletion = time.Now().Unix()
						case "request":
							file.UploadRequestId = s.Request.Id
						}
						protected := strings.HasPrefix(state, "password-") || state == "request-key" || state == "session-id" || state == "api-key"
						if protected {
							file.PasswordHash = hash
						}
						file.HotlinkId = rand.Text()
						database.SaveMetaData(file)
						if endpoint == "hotlink" {
							database.SaveHotlink(file)
						}
						blobPath := filepath.Join(s.DataDir, file.SHA1)
						beforeBlob, err := os.ReadFile(blobPath)
						if err != nil {
							t.Fatal(err)
						}
						if state == "missing" {
							database.DeleteMetaData(file.Id)
						}
						cookie := &http.Cookie{Name: "p" + file.Id}
						switch state {
						case "password-valid", "password-revoked":
							form := url.Values{"password": {password}}
							login := httptest.NewRequest("POST", "/d?id="+file.Id, strings.NewReader(form.Encode()))
							login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
							response := httptest.NewRecorder()
							showDownload(response, login)
							for _, c := range response.Result().Cookies() {
								if c.Name == cookie.Name {
									cookie = c
								}
							}
							if cookie.Value == "" {
								t.Fatal("correct password did not issue file capability")
							}
							if state == "password-revoked" {
								downloadPasswordToken.DeleteAllForFile(file.Id)
							}
						case "password-wrong-file":
							cookie.Value = downloadPasswordToken.Generate(s.Files["protected"].Id)
						case "request-key":
							cookie.Value = s.Keys["request"].Id
						case "api-key":
							cookie.Value = s.Keys["owner"].Id
						case "session-id":
							for id := range s.Sessions {
								cookie.Value = id
								break
							}
						}
						t.Cleanup(func() {
							downloadPasswordToken.DeleteAllForFile(file.Id)
							downloadPasswordToken.DeleteAllForFile(s.Files["protected"].Id)
						})
						mux := http.NewServeMux()
						mux.HandleFunc("/downloadFile", downloadFile)
						mux.HandleFunc("/dh/{id}/{name}", downloadFileWithNameInUrl)
						mux.HandleFunc("/h/", showHotlink)
						path := "/downloadFile?id=" + file.Id
						if endpoint == "filename" {
							path = "/dh/" + file.Id + "/synthetic.txt"
						}
						if endpoint == "hotlink" {
							path = "/h/" + file.HotlinkId
						}
						r := httptest.NewRequest("GET", path, nil)
						if cookie.Value != "" {
							r.AddCookie(cookie)
						}
						w := httptest.NewRecorder()
						mux.ServeHTTP(w, r)
						if allow {
							if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), beforeBlob) {
								t.Fatalf("valid capability failed: status=%d", w.Code)
							}
						} else {
							if bytes.Contains(w.Body.Bytes(), beforeBlob) {
								t.Fatal("denied public request returned file bytes")
							}
							if endpoint == "hotlink" {
								if !bytes.Equal(w.Body.Bytes(), imageExpiredPicture) {
									t.Fatal("invalid hotlink did not return expiry image")
								}
							} else if w.Code < 300 || w.Code >= 400 {
								t.Fatalf("expected denial redirect, status=%d", w.Code)
							}
							after, ok := database.GetMetaDataById(file.Id)
							if state == "missing" {
								if ok {
									t.Fatal("missing metadata resurrected")
								}
							} else if !ok || !reflect.DeepEqual(file, after) {
								t.Fatal("denied download changed metadata")
							}
						}
						afterBlob, err := os.ReadFile(blobPath)
						if err != nil || !bytes.Equal(beforeBlob, afterBlob) {
							t.Fatal("download changed or deleted blob")
						}
					})
				}
			}
		})
	}
}
