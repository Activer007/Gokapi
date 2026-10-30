//go:build test && !integration

package webserver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/presign"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
	"github.com/Activer007/SupaShare/internal/webserver/api"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
	"github.com/alicebob/miniredis/v2"
)

func collectionDownloadScene(t *testing.T) securityfixture.Scene {
	t.Helper()
	s := securityfixture.New(t, "")
	for _, name := range []string{"request", "own"} {
		file := s.Files[name]
		file.UploadRequestId, file.UnlimitedDownloads, file.DownloadsRemaining = s.Request.Id, false, 1
		s.Files[name] = file
	}
	s.Seed(presignTestStore{})
	oldDir := configuration.Get().DataDir
	configuration.Get().DataDir = s.DataDir
	t.Cleanup(func() { configuration.Get().DataDir = oldDir })
	return s
}

func collectionDownloadGrant(t *testing.T, key string, ids ...string) string {
	t.Helper()
	id := issueTestPresign(t, key, ids)
	t.Cleanup(func() {
		collectionRetries.Lock()
		lease := collectionRetries.grants[id]
		if lease != nil {
			lease.deadline = time.Now().Add(-time.Second)
		}
		collectionRetries.Unlock()
		if lease != nil {
			finishCollectionRetry(id, lease, false)
		}
	})
	return id
}

func collectionDownloadCall(id string, user models.User, method, rangeValue string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/downloadPresigned?key="+id, nil)
	if rangeValue != "" {
		r.Header.Set("Range", rangeValue)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	downloadPresigned(w, authentication.SetUserInRequest(r, user))
	return w
}

func collectionDownloadRemaining(t *testing.T, id string, want int) {
	t.Helper()
	file, ok := database.GetMetaDataById(id)
	if !ok || file.DownloadsRemaining != want {
		t.Fatalf("remaining=%d found=%v, want %d", file.DownloadsRemaining, ok, want)
	}
}

func TestCollectionDownloadPolicy(t *testing.T) {
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "collection.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "collection_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, tc := range []struct {
				name, issuer, consumer string
				zip, downgrade, super  bool
				remaining              int
			}{
				{name: "ordinary", issuer: "owner", consumer: "owner"},
				{name: "administrator", issuer: "owner", consumer: "admin", remaining: 1},
				{name: "super-administrator", issuer: "owner", consumer: "admin", super: true, remaining: 1},
				{name: "admin-issued-owner-consumed", issuer: "admin", consumer: "owner"},
				{name: "downgraded-consumer", issuer: "owner", consumer: "admin", downgrade: true},
				{name: "ordinary-zip", issuer: "owner", consumer: "owner", zip: true},
				{name: "administrator-zip", issuer: "owner", consumer: "admin", zip: true, remaining: 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := collectionDownloadScene(t)
					ids := []string{s.Files["request"].Id}
					if tc.zip {
						ids = append(ids, s.Files["own"].Id)
					}
					grant := collectionDownloadGrant(t, s.Keys[tc.issuer].Id, ids...)
					actor := s.Users[tc.consumer]
					current := actor
					if tc.downgrade {
						current.UserLevel = models.UserLevelUser
					}
					if tc.super {
						current.UserLevel = models.UserLevelSuperAdmin
					}
					database.SaveUser(current, false)
					w := collectionDownloadCall(grant, actor, http.MethodGet, "", nil)
					if w.Code != http.StatusOK {
						t.Fatalf("download status=%d", w.Code)
					}
					if tc.zip {
						z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
						if err != nil || len(z.File) != 2 {
							t.Fatal("invalid archive", err)
						}
					} else if w.Body.String() != "synthetic file request" {
						t.Fatal("download bytes differ")
					}
					for _, id := range ids {
						collectionDownloadRemaining(t, id, tc.remaining)
						file, _ := database.GetMetaDataById(id)
						if file.DownloadCount != 1-tc.remaining {
							t.Fatal("download counter disagrees with role exemption")
						}
					}
				})
			}
			t.Run("metadata-probes-preserve-grant", func(t *testing.T) {
				s := collectionDownloadScene(t)
				file := s.Files["request"]
				grant := collectionDownloadGrant(t, s.Keys["owner"].Id, file.Id)
				for _, probe := range []struct {
					method, rangeValue string
					status             int
				}{
					{http.MethodHead, "", http.StatusOK}, {http.MethodGet, "bytes=999-", http.StatusRequestedRangeNotSatisfiable},
				} {
					w := collectionDownloadCall(grant, s.Users["owner"], probe.method, probe.rangeValue, nil)
					if w.Code != probe.status {
						t.Fatalf("probe status=%d, want %d", w.Code, probe.status)
					}
					if probe.method == http.MethodHead && w.Body.Len() != 0 {
						t.Fatal("HEAD returned payload")
					}
					if _, ok := presign.Get(grant); !ok {
						t.Fatal("probe consumed the capability")
					}
					collectionDownloadRemaining(t, file.Id, 1)
				}
				if w := collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, "", nil); w.Code != http.StatusOK {
					t.Fatal("probe prevented subsequent download")
				}
				collectionDownloadRemaining(t, file.Id, 0)
			})
			t.Run("truncated-file-range-does-not-consume-allowance", func(t *testing.T) {
				s := collectionDownloadScene(t)
				file := s.Files["request"]
				grant := collectionDownloadGrant(t, s.Keys["owner"].Id, file.Id)
				// Metadata still describes the original file, so this Range is
				// within its declared size but beyond the truncated blob's EOF.
				if err := os.Truncate(filepath.Join(s.DataDir, file.SHA1), 3); err != nil {
					t.Fatal(err)
				}
				w := collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, "bytes=10-", nil)
				if w.Code < 400 || bytes.Contains(w.Body.Bytes(), []byte("synthetic file")) {
					t.Fatalf("truncated blob was served: status=%d", w.Code)
				}
				collectionDownloadRemaining(t, file.Id, 1)
				stored, _ := database.GetMetaDataById(file.Id)
				if stored.DownloadCount != 0 || downloadstatus.IsCurrentlyDownloading(file) {
					t.Fatal("rejected truncated blob consumed a download or retained a guard")
				}
			})
			t.Run("zip-member-preflight-failure-preserves-all-allowances", func(t *testing.T) {
				s := collectionDownloadScene(t)
				first, broken := s.Files["request"], s.Files["own"]
				grant := collectionDownloadGrant(t, s.Keys["owner"].Id, first.Id, broken.Id)
				// The path remains present, exercising the transfer preflight
				// rather than failing the earlier metadata lookup.
				if err := os.Truncate(filepath.Join(s.DataDir, broken.SHA1), 0); err != nil {
					t.Fatal(err)
				}
				w := collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, "", nil)
				if w.Code < 400 || w.Header().Get("Content-Type") == "application/zip" {
					t.Fatalf("invalid ZIP member was admitted: status=%d", w.Code)
				}
				for _, file := range []models.File{first, broken} {
					collectionDownloadRemaining(t, file.Id, 1)
					stored, _ := database.GetMetaDataById(file.Id)
					if stored.DownloadCount != 0 || downloadstatus.IsCurrentlyDownloading(file) {
						t.Fatal("ZIP preflight failure charged or retained a member")
					}
				}
			})
			t.Run("retry-window-holds-and-releases-cleanup-guard", func(t *testing.T) {
				s := collectionDownloadScene(t)
				file := s.Files["request"]
				grant := collectionDownloadGrant(t, s.Keys["owner"].Id, file.Id)
				w := collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, "", nil)
				if w.Code != http.StatusOK {
					t.Fatalf("download status=%d", w.Code)
				}
				collectionDownloadRemaining(t, file.Id, 0)
				if !downloadstatus.IsCurrentlyDownloading(file) {
					t.Fatal("last allowance was not protected during its retry window")
				}
				collectionRetries.Lock()
				lease := collectionRetries.grants[grant]
				if lease != nil {
					lease.deadline = time.Now().Add(-time.Second)
				}
				collectionRetries.Unlock()
				if lease == nil {
					t.Fatal("missing bounded retry lease")
				}
				finishCollectionRetry(grant, lease, false)
				if downloadstatus.IsCurrentlyDownloading(file) {
					t.Fatal("expired retry window retained its cleanup guard")
				}
				collectionRetries.Lock()
				_, retained := collectionRetries.grants[grant]
				collectionRetries.Unlock()
				if retained {
					t.Fatal("expired retry lease was not removed")
				}
			})
			t.Run("retry-authority", func(t *testing.T) {
				for _, mode := range []string{"range", "no-cookie", "other-user", "revoked-key", "pending", "expired-file", "expired-window", "replaced-blob"} {
					t.Run(mode, func(t *testing.T) {
						s := collectionDownloadScene(t)
						file, actor := s.Files["request"], s.Users["owner"]
						grant := collectionDownloadGrant(t, s.Keys["owner"].Id, file.Id)
						w := collectionDownloadCall(grant, actor, http.MethodGet, "", nil)
						if w.Code != http.StatusOK || len(w.Result().Cookies()) == 0 {
							t.Fatal("missing initial transfer or retry cookie")
						}
						cookie := w.Result().Cookies()[0]
						if !cookie.HttpOnly || cookie.MaxAge != int(collectionRetryWindow.Seconds()) {
							t.Fatal("unexpected retry cookie bounds")
						}
						collectionDownloadRemaining(t, file.Id, 0)
						switch mode {
						case "no-cookie":
							cookie = nil
						case "other-user":
							actor = s.Users["admin"] // Still authorized for the file, but not this consumer-bound grant.
						case "revoked-key":
							database.DeleteApiKey(s.Keys["owner"].Id)
						case "pending":
							file.PendingDeletion = time.Now().Add(time.Minute).Unix()
						case "expired-file":
							file.UnlimitedTime, file.ExpireAt = false, time.Now().Add(-time.Second).Unix()
						case "replaced-blob":
							file.SHA1 = s.Files["own"].SHA1
						case "expired-window":
							collectionRetries.Lock()
							collectionRetries.grants[grant].deadline = time.Now().Add(-time.Second)
							collectionRetries.Unlock()
						}
						if mode == "pending" || mode == "expired-file" || mode == "replaced-blob" {
							file.DownloadsRemaining = 0
							database.SaveMetaData(file)
						}
						retry := collectionDownloadCall(grant, actor, http.MethodGet, "bytes=10-", cookie)
						if mode == "range" {
							if retry.Code != http.StatusPartialContent || retry.Body.String() != "file request" {
								t.Fatalf("Range retry failed: status=%d bytes=%q", retry.Code, retry.Body.String())
							}
						} else if retry.Code < 400 || bytes.Contains(retry.Body.Bytes(), []byte("synthetic file")) {
							t.Fatalf("invalid retry succeeded: status=%d", retry.Code)
						}
						collectionDownloadRemaining(t, file.Id, 0)
					})
				}
			})
			t.Run("retry-rechecks-administrator-downgrade", func(t *testing.T) {
				s := collectionDownloadScene(t)
				file, actor := s.Files["request"], s.Users["admin"]
				grant := collectionDownloadGrant(t, s.Keys["owner"].Id, file.Id)
				initial := collectionDownloadCall(grant, actor, http.MethodGet, "", nil)
				if initial.Code != http.StatusOK || len(initial.Result().Cookies()) == 0 {
					t.Fatal("administrator transfer missing retry authorization")
				}
				collectionDownloadRemaining(t, file.Id, 1)
				downgraded := actor
				downgraded.UserLevel = models.UserLevelUser
				database.SaveUser(downgraded, false)
				for i := 0; i < 2; i++ {
					w := collectionDownloadCall(grant, actor, http.MethodGet, "bytes=10-", initial.Result().Cookies()[0])
					if w.Code != http.StatusPartialContent {
						t.Fatalf("downgraded retry status=%d", w.Code)
					}
					collectionDownloadRemaining(t, file.Id, 0)
				}
				stored, _ := database.GetMetaDataById(file.Id)
				if stored.DownloadCount != 1 {
					t.Fatal("role downgrade charged a retry more than once")
				}
			})
			t.Run("concurrent-last-allowance", func(t *testing.T) {
				s := collectionDownloadScene(t)
				file := s.Files["request"]
				grants := make([]string, 8)
				for i := range grants {
					grants[i] = collectionDownloadGrant(t, s.Keys["owner"].Id, file.Id)
				}
				var workers sync.WaitGroup
				var successes atomic.Int32
				start := make(chan struct{})
				for _, grant := range grants {
					workers.Add(1)
					go func(id string) {
						defer workers.Done()
						<-start
						w := collectionDownloadCall(id, s.Users["owner"], http.MethodGet, "", nil)
						if w.Code == http.StatusOK {
							successes.Add(1)
						} else if w.Code < 400 {
							t.Errorf("unexpected status=%d", w.Code)
						}
					}(grant)
				}
				close(start)
				workers.Wait()
				if successes.Load() != 1 {
					t.Fatalf("last allowance served %d downloads", successes.Load())
				}
				collectionDownloadRemaining(t, file.Id, 0)
			})
			t.Run("direct-api-cannot-disable-counting", func(t *testing.T) {
				s := collectionDownloadScene(t)
				for _, actor := range []string{"owner", "admin"} {
					file := s.Files["request"]
					file.DownloadsRemaining = 1
					database.SaveMetaData(file)
					r := httptest.NewRequest(http.MethodGet, "/api/files/download/"+file.Id, nil)
					r.Header.Set("apikey", s.Keys[actor].Id)
					r.Header.Set("increaseCounter", "false")
					w := httptest.NewRecorder()
					api.Process(w, r)
					if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "synthetic file request") {
						t.Fatalf("direct API status=%d", w.Code)
					}
					remaining := 0
					if actor == "admin" {
						remaining = 1
					}
					collectionDownloadRemaining(t, file.Id, remaining)
				}
			})
			t.Run("file-modify-cannot-disable-collection-limit", func(t *testing.T) {
				s := collectionDownloadScene(t)
				for _, tc := range []struct {
					file, value string
					status      int
				}{
					{"request", "0", http.StatusBadRequest}, {"request", "11", http.StatusBadRequest},
					{"request", "10", http.StatusOK}, {"protected", "0", http.StatusOK},
				} {
					file := s.Files[tc.file]
					r := httptest.NewRequest(http.MethodPut, "/api/files/modify", nil)
					r.Header.Set("apikey", s.Keys["owner"].Id)
					r.Header.Set("id", file.Id)
					r.Header.Set("allowedDownloads", tc.value)
					r.Header.Set("originalPassword", "true")
					w := httptest.NewRecorder()
					api.Process(w, r)
					if w.Code != tc.status {
						t.Fatalf("modify %s downloads=%s status=%d, want %d", tc.file, tc.value, w.Code, tc.status)
					}
					stored, _ := database.GetMetaDataById(file.Id)
					if tc.status == http.StatusBadRequest && (stored.UnlimitedDownloads || stored.DownloadsRemaining != 1) {
						t.Fatal("rejected collection modification changed its limit")
					}
					if tc.value == "10" && stored.DownloadsRemaining != 10 {
						t.Fatal("valid maximum modification did not persist")
					}
					if tc.file == "protected" && !stored.UnlimitedDownloads {
						t.Fatal("ordinary file lost unlimited-download support")
					}
				}
			})
			t.Run("file-duplicate-cannot-disable-collection-limit", func(t *testing.T) {
				s := collectionDownloadScene(t)
				request := s.Request
				request.MaxFiles = 10
				database.SaveFileRequest(request)
				for _, tc := range []struct {
					file, value string
					status      int
				}{
					{"request", "0", http.StatusBadRequest}, {"request", "11", http.StatusBadRequest},
					{"request", "10", http.StatusOK}, {"protected", "0", http.StatusOK},
				} {
					file := s.Files[tc.file]
					r := httptest.NewRequest(http.MethodPost, "/api/files/duplicate", nil)
					r.Header.Set("apikey", s.Keys["owner"].Id)
					r.Header.Set("id", file.Id)
					r.Header.Set("allowedDownloads", tc.value)
					r.Header.Set("originalPassword", "true")
					w := httptest.NewRecorder()
					api.Process(w, r)
					if w.Code != tc.status {
						t.Fatalf("duplicate %s downloads=%s status=%d, want %d", tc.file, tc.value, w.Code, tc.status)
					}
					collectionDownloadRemaining(t, file.Id, file.DownloadsRemaining)
					if tc.status == http.StatusBadRequest {
						continue
					}
					var result models.FileApiOutput
					if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Id == "" {
						t.Fatalf("missing duplicate output: %v", err)
					}
					copy, found := database.GetMetaDataById(result.Id)
					if !found || copy.UploadRequestId != file.UploadRequestId {
						t.Fatal("duplicate did not preserve its collection identity")
					}
					if tc.file == "request" && (copy.UnlimitedDownloads || copy.DownloadsRemaining != 10) {
						t.Fatal("collection duplicate did not retain the valid maximum")
					}
					if tc.file == "protected" && !copy.UnlimitedDownloads {
						t.Fatal("ordinary duplicate lost unlimited-download support")
					}
				}
			})
		})
	}
}
