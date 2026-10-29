//go:build test

package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/storage"
	"github.com/Activer007/Gokapi/internal/storage/chunking/chunksession"
	"github.com/Activer007/Gokapi/internal/storage/filesystem"
	"github.com/Activer007/Gokapi/internal/webserver/errorHandling/errorcodes"
)

// Characterize the existing public contract before introducing a separate total
// budget. All data is synthetic; the provider-global API fixture runs in a child.
func TestSEC031UploadSizeSemantics(t *testing.T) {
	const childEnv = "GOKAPI_SEC031_SEMANTICS_CHILD"
	if os.Getenv(childEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestSEC031UploadSizeSemantics$", "-test.v")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), childEnv+"="+backend)
			output, err := cmd.CombinedOutput()
			t.Logf("isolated characterization:\n%s", output)
			if err != nil {
				t.Fatalf("characterization failed: %v", err)
			}
		}
		return
	}
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		if os.Getenv(childEnv) != backend {
			continue
		}
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "semantics.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "sec031_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			oldDir := configuration.Get().DataDir
			configuration.Get().DataDir = t.TempDir()
			filesystem.Init(configuration.Get().DataDir)
			t.Cleanup(func() {
				configuration.Get().DataDir = oldDir
				filesystem.Init(oldDir)
			})
			user := models.User{Id: 9031, Name: "synthetic-uploader", UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads}
			key := models.ApiKey{Id: rand.Text(), UserId: user.Id, Permissions: models.ApiPermUpload}
			request := models.FileRequest{Id: rand.Text(), UserId: user.Id, ApiKey: key.Id, MaxFiles: 2, MaxSize: 1, Expiry: time.Now().Add(time.Hour).Unix()}
			key.UploadRequestId = request.Id
			database.SaveUser(user, false)
			database.SaveApiKey(key)
			database.SaveFileRequest(request)
			call := func(path string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/api"+path, body)
				r.Header.Set("apikey", key.Id)
				for k, v := range headers {
					r.Header.Set(k, v)
				}
				w := httptest.NewRecorder()
				Process(w, r)
				return w
			}
			checkError := func(w *httptest.ResponseRecorder, status, code int) string {
				t.Helper()
				var result struct {
					ErrorCode    int
					ErrorMessage string
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != status || result.ErrorCode != code {
					t.Fatalf("wrong rejection: status=%d code=%d, want status=%d code=%d", w.Code, result.ErrorCode, status, code)
				}
				return result.ErrorMessage
			}
			reserve := func() string {
				w := call("/uploadrequest/chunk/reserve", nil, map[string]string{"id": request.Id})
				var result struct{ Uuid string }
				if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Uuid == "" {
					t.Fatalf("reserve failed: status=%d", w.Code)
				}
				t.Cleanup(func() {
					if err := storage.ChunkSessions().Cancel(context.Background(), chunksession.Scope{ClientID: result.Uuid, UserID: user.Id, RequestID: request.Id}); err != nil {
						t.Error("synthetic request reservation cleanup failed", err)
					}
				})
				return result.Uuid
			}
			chunk := func(uuid string, payload []byte, unknownLength bool) *httptest.ResponseRecorder {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				for k, v := range map[string]string{"uuid": uuid, "filesize": strconv.Itoa(len(payload)), "offset": "0"} {
					if err := writer.WriteField(k, v); err != nil {
						t.Fatal(err)
					}
				}
				part, err := writer.CreateFormFile("file", "synthetic.bin")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(payload); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				var reader io.Reader = &body
				if unknownLength {
					reader = struct{ io.Reader }{&body}
				}
				return call("/uploadrequest/chunk/add", reader, map[string]string{"fileRequestId": request.Id, "Content-Type": writer.FormDataContentType()})
			}
			t.Run("expired-request", func(t *testing.T) {
				expired := request
				expired.Expiry = time.Now().Add(-time.Minute).Unix()
				database.SaveFileRequest(expired)
				defer database.SaveFileRequest(request)
				checkError(call("/uploadrequest/chunk/reserve", nil, map[string]string{"id": request.Id}), http.StatusUnauthorized, errorcodes.RequestExpired)
			})

			// Each file is below 1 MiB; together they exceed it. This is a
			// characterization of MaxSize, not proof of a new total quota.
			expected := make(map[string][]byte)
			for i := 0; i < 2; i++ {
				uuid := reserve()
				if i == 0 {
					t.Run("minimum-free-space", func(t *testing.T) {
						probeID := reserve()
						defer func() {
							if err := storage.ChunkSessions().Cancel(context.Background(), chunksession.Scope{ClientID: probeID, UserID: user.Id, RequestID: request.Id}); err != nil {
								t.Error(err)
							}
						}()
						// A synthetic floor above this isolated host's capacity;
						// never allocate or fill a real disk to test refusal.
						t.Setenv("GOKAPI_MIN_FREE_SPACE", "2147483647")
						message := checkError(chunk(probeID, []byte("abc"), false), http.StatusInsufficientStorage, errorcodes.CannotAllocateFile)
						if !strings.Contains(message, "space") {
							t.Fatal("upload was not rejected by the disk-space check")
						}
					})
					for _, test := range []struct {
						name    string
						payload int
						unknown bool
					}{{"over-per-file-limit", 1024*1024 + 1, false}} {
						t.Run(test.name, func(t *testing.T) {
							w := chunk(uuid, bytes.Repeat([]byte{0x31}, test.payload), test.unknown)
							code := errorcodes.FileTooLarge
							if test.unknown {
								code = errorcodes.ChunkTooSmall
							}
							checkError(w, http.StatusBadRequest, code)
							files, err := os.ReadDir(configuration.Get().DataDir)
							if err != nil {
								t.Fatal("rejected upload created staged data")
							}
							for _, entry := range files {
								if entry.Name() != ".gokapi-receive-v1" && entry.Name() != ".gokapi-chunks-v1" {
									t.Fatal("rejected upload created chunk or blob data")
								}
								owned, err := os.ReadDir(filepath.Join(configuration.Get().DataDir, entry.Name()))
								if err != nil || len(owned) != 1 || owned[0].Name() != ".owner" || !owned[0].Type().IsRegular() {
									t.Fatal("rejected upload left receive/session payload or unexpected spool entry")
								}
							}
							ledger, err := database.ReadUploadQuota(context.Background())
							if err != nil {
								t.Fatal(err)
							}
							record, found := ledger.Records[chunksession.ID(chunksession.Scope{ClientID: uuid, UserID: user.Id, RequestID: request.Id})]
							if !found || record.BytesKnown || record.Bytes != 0 || record.StagingBytes != 0 {
								t.Fatal("over-limit body changed its existing unbound reservation")
							}
						})
					}
					// MaxSize applies to payload; bounded multipart overhead and an
					// unknown HTTP length no longer displace a valid last chunk.
					for _, item := range []struct {
						name    string
						size    int
						unknown bool
					}{{"exact-payload-limit", 1024 * 1024, false}, {"unknown-length-last-chunk", 3, true}} {
						t.Run(item.name, func(t *testing.T) {
							probeID := reserve()
							payload := bytes.Repeat([]byte{0x32}, item.size)
							w := chunk(probeID, payload, item.unknown)
							if w.Code != http.StatusOK {
								t.Fatalf("valid payload rejected: status=%d", w.Code)
							}
							assertRuntimeChunkBytes(t, chunksession.Scope{ClientID: probeID, UserID: user.Id, RequestID: request.Id}, payload)
						})
					}
				}
				payload := bytes.Repeat([]byte{byte(i + 1)}, 768*1024)
				if w := chunk(uuid, payload, false); w.Code != http.StatusOK {
					t.Fatalf("valid chunk rejected: status=%d", w.Code)
				}
				w := call("/uploadrequest/chunk/complete", nil, map[string]string{"fileRequestId": request.Id, "uuid": uuid, "filename": "synthetic.bin", "filesize": strconv.Itoa(len(payload))})
				if w.Code != http.StatusOK {
					t.Fatalf("valid complete rejected: status=%d", w.Code)
				}
				var result struct{ FileInfo models.FileApiOutput }
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.FileInfo.Id == "" {
					t.Fatal("missing completion identity")
				}
				if _, exists := expected[result.FileInfo.Id]; exists {
					t.Fatal("independent uploads reused a metadata identity")
				}
				expected[result.FileInfo.Id] = payload
			}
			var total int64
			var count int
			blobs := make(map[string]bool)
			for _, file := range database.GetAllMetadata() {
				if file.UploadRequestId != request.Id {
					continue
				}
				count++
				total += file.SizeBytes
				data, err := os.ReadFile(filepath.Join(configuration.Get().DataDir, file.SHA1))
				if err != nil || !bytes.Equal(data, expected[file.Id]) || len(data) != 768*1024 {
					t.Fatal("completed bytes differ from uploaded data")
				}
				blobs[file.SHA1] = true
			}
			if count != 2 || total != 1536*1024 || len(blobs) != 2 {
				t.Fatalf("MaxSize characterization changed: files=%d bytes=%d", count, total)
			}
			checkError(call("/uploadrequest/chunk/reserve", nil, map[string]string{"id": request.Id}), http.StatusUnauthorized, errorcodes.CannotUploadMoreFiles)
			t.Logf("MaxSize=1 MiB accepted %d files totalling %d bytes; sequential MaxFiles=2 enforced", count, total)
		})
	}
}
