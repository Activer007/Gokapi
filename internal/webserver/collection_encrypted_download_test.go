//go:build test && !integration

package webserver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/presign"
	"github.com/Activer007/SupaShare/internal/webserver/api"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
	"github.com/alicebob/miniredis/v2"
)

func TestCollectionEncryptedDownloadSize(t *testing.T) {
	master, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal(err)
	}
	encryption.Init(models.Configuration{Encryption: models.Encryption{Level: encryption.LocalEncryptionStored, Cipher: master}})
	plaintext := bytes.Repeat([]byte("synthetic collection payload"), 8192)
	var info models.EncryptionInfo
	var ciphertext bytes.Buffer
	if err := encryption.Encrypt(&info, bytes.NewReader(plaintext), &ciphertext); err != nil {
		t.Fatal(err)
	}
	if int64(ciphertext.Len()) != encryption.CalculateEncryptedFilesize(int64(len(plaintext))) {
		t.Fatal("actual encrypted fixture disagrees with the storage size calculation")
	}
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "encrypted-collection.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "encrypted_collection_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, tc := range []struct {
				name      string
				zip, e2e  bool
				sizeDelta int
			}{
				{name: "intact-single"}, {name: "truncated-single", sizeDelta: -1},
				{name: "intact-zip", zip: true}, {name: "truncated-zip", zip: true, sizeDelta: -1},
				{name: "oversized-single", sizeDelta: 1}, {name: "e2e-raw-size", e2e: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := collectionDownloadScene(t)
					file := s.Files["request"]
					file.Encryption, file.SizeBytes = info, int64(len(plaintext))
					payload := append([]byte(nil), ciphertext.Bytes()...)
					if tc.e2e {
						file.Encryption = models.EncryptionInfo{IsEncrypted: true, IsEndToEndEncrypted: true}
						file.SizeBytes = int64(len(payload))
					}
					if tc.sizeDelta < 0 {
						payload = payload[:len(payload)+tc.sizeDelta]
					} else if tc.sizeDelta > 0 {
						payload = append(payload, 0)
					}
					if err := os.WriteFile(filepath.Join(s.DataDir, file.SHA1), payload, 0600); err != nil {
						t.Fatal(err)
					}
					database.SaveMetaData(file)
					ids := []string{file.Id}
					if tc.zip {
						// The healthy member must retain its allowance if any other
						// member has an unreadable encrypted representation.
						ids = append([]string{s.Files["own"].Id}, ids...)
					}
					grant := collectionDownloadGrant(t, s.Keys["owner"].Id, ids...)
					w := collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, "", nil)
					if tc.sizeDelta != 0 {
						if w.Code < 400 || bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) || bytes.Contains(w.Body.Bytes(), plaintext[:64]) {
							t.Errorf("invalid ciphertext was served: status=%d", w.Code)
						}
						for _, id := range ids {
							stored, _ := database.GetMetaDataById(id)
							if stored.DownloadsRemaining != 1 || stored.DownloadCount != 0 {
								t.Errorf("invalid ciphertext charged member %q: remaining=%d count=%d", stored.Name, stored.DownloadsRemaining, stored.DownloadCount)
							}
						}
						return
					}
					if w.Code != http.StatusOK {
						t.Fatalf("intact download status=%d", w.Code)
					}
					if tc.zip {
						archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
						if err != nil || len(archive.File) != 2 {
							t.Fatal("intact archive is invalid", err)
						}
						found := false
						for _, entry := range archive.File {
							if entry.Name != file.Name {
								continue
							}
							handle, err := entry.Open()
							if err != nil {
								t.Fatal(err)
							}
							data, err := io.ReadAll(handle)
							_ = handle.Close()
							if err != nil || !bytes.Equal(data, plaintext) {
								t.Fatal("archive plaintext differs", err)
							}
							found = true
						}
						if !found {
							t.Fatal("archive omitted its encrypted member")
						}
					} else {
						want := plaintext
						if tc.e2e {
							want = payload
						}
						if !bytes.Equal(w.Body.Bytes(), want) {
							t.Fatal("intact download bytes differ")
						}
					}
					for _, id := range ids {
						collectionDownloadRemaining(t, id, 0)
						stored, _ := database.GetMetaDataById(id)
						if stored.DownloadCount != 1 {
							t.Fatal("intact transfer did not count exactly once")
						}
					}
				})
			}
		})
	}
}

func TestCollectionE2EArchivePreflight(t *testing.T) {
	// E2E ciphertext is complete and valid, but its key is never given to the
	// server. An archive must refuse it before charging another healthy member.
	reader, err := encryption.GetEncryptReader(bytes.Repeat([]byte{0x3a}, 32), strings.NewReader("synthetic E2E payload"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "e2e-archive.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "e2e_archive_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, tc := range []struct {
				name                         string
				zip, direct, mixed, ordinary bool
			}{
				{name: "presigned-zip", zip: true, mixed: true}, {name: "direct-zip", zip: true, direct: true, mixed: true},
				{name: "presigned-single-zip", zip: true}, {name: "direct-single-zip", zip: true, direct: true},
				{name: "presigned-ordinary-single-zip", zip: true, ordinary: true}, {name: "direct-ordinary-single-zip", zip: true, direct: true, ordinary: true},
				{name: "presigned-raw-single"}, {name: "direct-raw-single", direct: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := collectionDownloadScene(t)
					e2e := s.Files["request"]
					ids := []string{e2e.Id}
					if tc.mixed {
						ids = append(ids, s.Files["own"].Id)
					}
					grant := ""
					if tc.zip && !tc.direct {
						// Issue while all members are supported; consumption must
						// still reject a member that later becomes E2E encrypted.
						grant = collectionArchiveGrant(t, s.Keys["owner"].Id, ids)
					}
					e2e.Encryption = models.EncryptionInfo{IsEncrypted: true, IsEndToEndEncrypted: true}
					e2e.SizeBytes = int64(len(payload))
					if tc.mixed || tc.ordinary {
						// A real mixed archive can contain an ordinary E2E upload
						// alongside a file collected through the public upload page.
						e2e.UploadRequestId = ""
					}
					if err := os.WriteFile(filepath.Join(s.DataDir, e2e.SHA1), payload, 0600); err != nil {
						t.Fatal(err)
					}
					database.SaveMetaData(e2e)
					if tc.zip {
						issuance := collectionArchiveAPI(s.Keys["owner"].Id, ids, true, "")
						var failure struct{ ErrorCode int }
						if issuance.Code != http.StatusBadRequest || json.Unmarshal(issuance.Body.Bytes(), &failure) != nil || failure.ErrorCode != errorcodes.EndToEndNotSupported {
							t.Errorf("unsupported archive received a new presign: status=%d", issuance.Code)
						}
					}
					var w *httptest.ResponseRecorder
					if tc.direct {
						if tc.zip {
							w = collectionArchiveAPI(s.Keys["owner"].Id, ids, false, "")
						} else {
							path := "/api/files/download/" + e2e.Id
							r := httptest.NewRequest(http.MethodGet, path, nil)
							r.Header.Set("apikey", s.Keys["owner"].Id)
							r.Header.Set("ids", strings.Join(ids, ","))
							w = httptest.NewRecorder()
							api.Process(w, r)
						}
					} else {
						if !tc.zip {
							grant = collectionDownloadGrant(t, s.Keys["owner"].Id, ids...)
						}
						w = collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, "", nil)
					}
					if tc.zip {
						if w.Code < 400 || bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
							t.Errorf("unsupported E2E archive was served: status=%d", w.Code)
						}
						for _, id := range ids {
							file, ok := database.GetMetaDataById(id)
							if !ok || file.DownloadsRemaining != 1 || file.DownloadCount != 0 || downloadstatus.IsCurrentlyDownloading(file) {
								t.Errorf("rejected archive changed member %q or left a download guard: remaining=%d count=%d", file.Name, file.DownloadsRemaining, file.DownloadCount)
							}
						}
						return
					}
					if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), payload) {
						t.Fatalf("E2E raw single download failed: status=%d", w.Code)
					}
					collectionDownloadRemaining(t, e2e.Id, 0)
				})
			}
		})
	}
}

func collectionArchiveAPI(key string, ids []string, issue bool, rangeValue string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/files/downloadzip", nil)
	r.Header.Set("apikey", key)
	r.Header.Set("ids", strings.Join(ids, ","))
	if issue {
		r.Header.Set("presignUrl", "true")
	}
	if rangeValue != "" {
		r.Header.Set("Range", rangeValue)
	}
	w := httptest.NewRecorder()
	api.Process(w, r)
	return w
}

func collectionArchiveGrant(t *testing.T, key string, ids []string) string {
	t.Helper()
	w := collectionArchiveAPI(key, ids, true, "")
	var result struct {
		URL string `json:"downloadUrl"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("archive issuance status=%d", w.Code)
	}
	u, err := url.Parse(result.URL)
	if err != nil || u.Query().Get("key") == "" {
		t.Fatal("archive issuance omitted its capability", err)
	}
	id := u.Query().Get("key")
	grant, found := presign.Get(id)
	if !found || !grant.Archive {
		t.Fatal("archive endpoint lost the explicit output mode")
	}
	t.Cleanup(func() { presign.Delete(id) })
	trackCollectionDownloadGrant(t, id)
	return id
}

func TestCollectionExplicitArchive(t *testing.T) {
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "explicit-archive.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "explicit_archive_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, tc := range []struct {
				name                          string
				direct, ordinary, legacyMulti bool
			}{
				{name: "presigned-single"}, {name: "direct-single", direct: true},
				{name: "presigned-ordinary-single", ordinary: true}, {name: "direct-ordinary-single", direct: true, ordinary: true},
				{name: "legacy-presigned-multi", legacyMulti: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := collectionDownloadScene(t)
					file := s.Files["request"]
					if tc.ordinary {
						file = s.Files["protected"]
						file.UnlimitedDownloads, file.DownloadsRemaining = false, 1
						database.SaveMetaData(file)
					}
					ids := []string{file.Id}
					if tc.legacyMulti {
						ids = append(ids, s.Files["own"].Id)
					}
					grant := ""
					if !tc.direct {
						grant = collectionArchiveGrant(t, s.Keys["owner"].Id, ids)
						if tc.legacyMulti {
							legacy, _ := presign.Get(grant)
							legacy.Archive = false
							presign.Save(legacy)
						}
					}
					call := func(rangeValue string, cookie *http.Cookie) *httptest.ResponseRecorder {
						if tc.direct {
							return collectionArchiveAPI(s.Keys["owner"].Id, ids, false, rangeValue)
						}
						return collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, rangeValue, cookie)
					}
					if !tc.ordinary {
						if probe := call("bytes=0-1", nil); probe.Code != http.StatusRequestedRangeNotSatisfiable {
							t.Fatalf("dynamic single-file archive accepted Range: status=%d", probe.Code)
						}
						collectionDownloadRemaining(t, file.Id, 1)
						if !tc.direct {
							head := collectionDownloadCall(grant, s.Users["owner"], http.MethodHead, "", nil)
							if head.Code != http.StatusOK || head.Header().Get("Content-Type") != "application/zip" || head.Body.Len() != 0 {
								t.Fatal("archive HEAD lost the explicit format")
							}
							if _, valid := presign.Get(grant); !valid {
								t.Fatal("archive probes consumed the capability")
							}
						}
					}
					assertArchive := func(w *httptest.ResponseRecorder) {
						t.Helper()
						archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
						if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" || err != nil || len(archive.File) != len(ids) {
							t.Fatalf("single-file archive lost its ZIP format: status=%d error=%v", w.Code, err)
						}
						entry := archive.File[0]
						for _, candidate := range archive.File {
							if candidate.Name == file.Name {
								entry = candidate
							}
						}
						handle, err := entry.Open()
						if err != nil {
							t.Fatal(err)
						}
						data, err := io.ReadAll(handle)
						_ = handle.Close()
						want, readErr := os.ReadFile(filepath.Join(s.DataDir, file.SHA1))
						if err != nil || readErr != nil || !bytes.Equal(data, want) {
							t.Fatal("single-file archive changed its payload")
						}
					}
					w := call("", nil)
					assertArchive(w)
					want := 0
					if tc.ordinary {
						want = 1
					}
					collectionDownloadRemaining(t, file.Id, want)
					if !tc.direct && !tc.ordinary {
						cookies := w.Result().Cookies()
						if len(cookies) == 0 {
							t.Fatal("collection archive omitted its bounded retry authorization")
						}
						if probe := call("bytes=0-1", cookies[0]); probe.Code != http.StatusRequestedRangeNotSatisfiable {
							t.Fatal("archive retry accepted Range")
						}
						assertArchive(call("", cookies[0]))
						collectionDownloadRemaining(t, file.Id, 0)
					}
				})
			}
		})
	}
}
