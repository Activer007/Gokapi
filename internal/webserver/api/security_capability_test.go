//go:build test

package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/presign"
	"github.com/forceu/gokapi/internal/test/securityfixture"
	"github.com/forceu/gokapi/internal/webserver/authentication/downloadPasswordToken"
)

// Enumerate the actual routing table, including future additions. Public
// capabilities and session IDs are not interchangeable with management keys.
func TestSEC020CapabilityEveryRoute(t *testing.T) {
	// Existing API tests leave asynchronous deletion/GC work running. Never
	// replace their global DB: run this provider matrix in a separate process
	// and working directory, using the same (also race-instrumented) binary.
	const childEnv = "GOKAPI_SEC020_CAPABILITY_CHILD"
	if os.Getenv(childEnv) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestSEC020CapabilityEveryRoute$", "-test.v")
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), childEnv+"=1")
		output, err := cmd.CombinedOutput()
		t.Logf("isolated provider matrix:\n%s", output)
		if err != nil {
			t.Fatalf("isolated matrix failed: %v", err)
		}
		return
	}
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "capabilities.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "cap_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			s := securityfixture.New(t, "")
			ownerKey := s.Keys["owner"]
			ownerKey.PublicId = (rand.Text() + rand.Text())[:LengthPublicId]
			s.Keys["owner"] = ownerKey
			oldDir := configuration.Get().DataDir
			configuration.Get().DataDir = s.DataDir
			t.Cleanup(func() { configuration.Get().DataDir = oldDir })
			blobs := make(map[string][]byte)
			for _, file := range s.Files {
				data, err := os.ReadFile(filepath.Join(s.DataDir, file.SHA1))
				if err != nil {
					t.Fatal(err)
				}
				blobs[file.SHA1] = data
			}
			for _, user := range s.Users {
				database.SaveUser(user, false)
			}
			for _, key := range s.Keys {
				database.SaveApiKey(key)
			}
			for id, session := range s.Sessions {
				database.SaveSession(id, session)
			}
			for _, file := range s.Files {
				database.SaveMetaData(file)
			}
			database.SaveFileRequest(s.Request)
			file := s.Files["own"]
			hotlink := rand.Text()
			hotlinkFile := file
			hotlinkFile.HotlinkId = hotlink
			database.SaveHotlink(hotlinkFile)
			pwToken := downloadPasswordToken.Generate(file.Id)
			t.Cleanup(func() { downloadPasswordToken.DeleteAllForFile(file.Id) })
			grant := models.Presign{Id: rand.Text(), FileIds: []string{file.Id}, Expiry: time.Now().Add(time.Minute).Unix(), UserId: file.UserId, ApiKeyId: s.Keys["owner"].Id}
			presign.Save(grant)
			t.Cleanup(func() { presign.Delete(grant.Id) })
			var sessionID string
			for id := range s.Sessions {
				sessionID = id
				break
			}
			credentials := map[string]string{
				"anonymous": "", "share-id": file.Id, "hotlink": hotlink,
				"password-cookie": pwToken, "presign": grant.Id, "session-id": sessionID,
				"key-public-id": s.Keys["owner"].PublicId, "request-id": s.Request.Id,
				"request-key":         s.Keys["request"].Id,
				"session-cookie-only": "", "password-cookie-only": "",
			}
			for _, route := range routes {
				for name, credential := range credentials {
					if name == "request-key" && route.IsFileRequestApi {
						continue
					}
					t.Run(route.Url+"/"+name, func(t *testing.T) {
						path := "/api" + route.Url
						if route.HasWildcard {
							path += file.Id
						}
						r := httptest.NewRequest("POST", path, nil)
						r.Header.Set("apikey", credential)
						if name == "session-cookie-only" {
							r.AddCookie(&http.Cookie{Name: "session_token", Value: sessionID})
						}
						if name == "password-cookie-only" {
							r.AddCookie(&http.Cookie{Name: "p" + file.Id, Value: pwToken})
						}
						w := httptest.NewRecorder()
						Process(w, r)
						if w.Code != 401 {
							t.Fatalf("capability reached route: status=%d", w.Code)
						}
					})
				}
			}
			// Positive controls prove that the fixture and auth boundary are live.
			for _, kind := range []string{"owner", "temporary"} {
				r := httptest.NewRequest("POST", "/api/info/version", nil)
				r.Header.Set("apikey", s.Keys[kind].Id)
				w := httptest.NewRecorder()
				Process(w, r)
				if w.Code != 200 {
					t.Fatalf("valid %s key rejected: %d", kind, w.Code)
				}
			}
			for _, route := range routes {
				if !route.IsFileRequestApi {
					continue
				}
				r := httptest.NewRequest("POST", "/api"+route.Url, nil)
				r.Header.Set("apikey", s.Keys["request"].Id)
				if _, _, ok := isAuthorisedForApi(r, route); !ok {
					t.Fatal("valid request key rejected at upload auth boundary")
				}
			}
			for _, before := range s.Files {
				after, ok := database.GetMetaDataById(before.Id)
				if !ok || !reflect.DeepEqual(before, after) {
					t.Fatal("denied API calls changed file metadata")
				}
				data, err := os.ReadFile(filepath.Join(s.DataDir, before.SHA1))
				if err != nil || !bytes.Equal(data, blobs[before.SHA1]) {
					t.Fatal("denied API calls changed or removed blob")
				}
			}
			if !downloadPasswordToken.IsValid(pwToken, file.Id) {
				t.Fatal("API rejection consumed password grant")
			}
			if _, ok := presign.Get(grant.Id); !ok {
				t.Fatal("API rejection consumed presign")
			}
		})
	}
}
