//go:build test && security_whole && !integration

package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/encryption"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/filesystem"
	"github.com/forceu/gokapi/internal/uploadquota"
)

const sec030WholeChildEnv = "GOKAPI_SEC030_WHOLE_QUOTA_CHILD"
const sec030WholeQuotaCode = 21
const sec030WholeBusyCode = 20
const sec030WholeOwner = 9030

// This contract deliberately uses only existing production symbols. Before
// runtime integration, the new policy environment is ignored and too many real
// uploads succeed: the RED is an accounting failure, not a compilation failure.
func TestSEC030WholeQuotaAdmission(t *testing.T) {
	if os.Getenv(sec030WholeChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, dimension := range []string{"bytes", "files"} {
				for _, contenders := range []int{10, 50} {
					name := fmt.Sprintf("%s/%s/%d", backend, dimension, contenders)
					t.Run(name, func(t *testing.T) {
						sec030WholeRunChild(t, "TestSEC030WholeQuotaAdmission", name, dimension)
					})
				}
			}
		}
		return
	}
	parts := strings.Split(os.Getenv(sec030WholeChildEnv), "/")
	if len(parts) != 3 || (parts[1] != "bytes" && parts[1] != "files") {
		t.Fatal("WHOLE_QUOTA_SETUP invalid isolated admission case")
	}
	contenders, err := strconv.Atoi(parts[2])
	if err != nil || (contenders != 10 && contenders != 50) {
		t.Fatal("WHOLE_QUOTA_SETUP invalid contender count")
	}
	connection := sec030WholeSetup(t, parts[0])
	seedContent := []byte("seed")
	if parts[1] == "bytes" {
		seedContent = bytes.Repeat([]byte{0x73}, 1024*1024-1)
	}
	seed := sec030WholeSeed(t, seedContent)
	keys := make([]models.ApiKey, contenders)
	forms := make([]sec030WholeForm, contenders)
	for i := range keys {
		keys[i] = sec030WholeKey(t)
		forms[i] = sec030WholeMultipart(t, []byte{byte(i + 1)})
	}
	for _, key := range keys {
		if stored, ok := database.GetApiKey(key.Id); !ok || stored.UserId != sec030WholeOwner {
			t.Fatal("WHOLE_QUOTA_SETUP distinct upload credentials did not persist")
		}
	}

	// All contenders first acquire reception slots and reach their first body
	// read. Releasing them together exercises the post-reception quota boundary;
	// receiver backpressure cannot substitute for durable quota enforcement.
	ready := make(chan struct{}, contenders)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = &sec030WholeGateBody{ReadCloser: r.Body, ctx: r.Context(), ready: ready, release: release}
		Process(w, r)
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type outcome struct {
		index int
		value sec030WholeResponse
	}
	outcomes := make(chan outcome, contenders)
	var workers sync.WaitGroup
	for i := range keys {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			outcomes <- outcome{index: i, value: sec030WholeSend(ctx, client, server.URL, keys[i].Id, forms[i])}
		}(i)
	}
	arrivals := 0
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
waitForBodies:
	for arrivals < contenders {
		select {
		case <-ready:
			arrivals++
		case <-timer.C:
			break waitForBodies
		case <-ctx.Done():
			break waitForBodies
		}
	}
	unblock()
	workers.Wait()
	close(outcomes)
	if arrivals != contenders {
		t.Errorf("WHOLE_QUOTA_RECEPTION only %d/%d contenders reached the body barrier", arrivals, contenders)
	}
	successes, quotaDenied, busy := 0, 0, 0
	expected := make(map[string][]byte)
	for result := range outcomes {
		response := result.value
		if response.failed {
			t.Error("WHOLE_QUOTA_TRANSPORT request or bounded JSON response failed")
			continue
		}
		if response.status == http.StatusTooManyRequests || response.ErrorCode == sec030WholeBusyCode {
			busy++
			t.Error("WHOLE_QUOTA_RECEPTION receiver backpressure is not a quota rejection")
			continue
		}
		if response.status == http.StatusOK && response.Result == "OK" && response.FileInfo.Id != "" {
			successes++
			if _, exists := expected[response.FileInfo.Id]; exists {
				t.Error("WHOLE_QUOTA_ID distinct requests reused a metadata identity")
			}
			expected[response.FileInfo.Id] = []byte{byte(result.index + 1)}
			continue
		}
		if sec030WholeIsQuota(response) {
			quotaDenied++
			continue
		}
		t.Errorf("WHOLE_QUOTA_RESPONSE unexpected status=%d code=%d", response.status, response.ErrorCode)
	}
	if successes != 1 || quotaDenied != contenders-1 || busy != 0 {
		t.Errorf("WHOLE_QUOTA_CAPACITY successes=%d quota_denied=%d receiver_busy=%d; want 1/%d/0", successes, quotaDenied, busy, contenders-1)
	}
	sec030WholeCheckFiles(t, expected, &seed, seedContent)
	sec030WholeCheckLedger(t, expected)
	database.Close()
	database.Connect(connection)
	sec030WholeCheckFiles(t, expected, &seed, seedContent)
	sec030WholeCheckLedger(t, expected)
}

func TestSEC030WholeQuotaSharedBlob(t *testing.T) {
	if os.Getenv(sec030WholeChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			t.Run(backend, func(t *testing.T) {
				sec030WholeRunChild(t, "TestSEC030WholeQuotaSharedBlob", backend+"/shared/0", "shared")
			})
		}
		return
	}
	parts := strings.Split(os.Getenv(sec030WholeChildEnv), "/")
	if len(parts) != 3 || parts[1] != "shared" {
		t.Fatal("WHOLE_QUOTA_SETUP invalid isolated shared-blob case")
	}
	connection := sec030WholeSetup(t, parts[0])
	server := httptest.NewServer(http.HandlerFunc(Process))
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 30 * time.Second
	payload := bytes.Repeat([]byte{0x62}, 768*1024)
	form := sec030WholeMultipart(t, payload)
	expected := make(map[string][]byte)
	for i := 0; i < 3; i++ {
		key := sec030WholeKey(t)
		response := sec030WholeSend(context.Background(), client, server.URL, key.Id, form)
		if response.failed {
			t.Fatal("WHOLE_QUOTA_TRANSPORT shared upload request or JSON response failed")
		}
		if i < 2 {
			if response.status != http.StatusOK || response.Result != "OK" || response.FileInfo.Id == "" {
				t.Fatalf("WHOLE_QUOTA_SHARED valid upload status=%d code=%d", response.status, response.ErrorCode)
			}
			if _, exists := expected[response.FileInfo.Id]; exists {
				t.Fatal("WHOLE_QUOTA_SHARED independent uploads reused a metadata identity")
			}
			expected[response.FileInfo.Id] = payload
		} else if !sec030WholeIsQuota(response) {
			t.Errorf("WHOLE_QUOTA_SHARED third upload status=%d code=%d; want 413/21", response.status, response.ErrorCode)
		}
	}
	sec030WholeCheckFiles(t, expected, nil, nil)
	sec030WholeCheckLedger(t, expected)
	files := database.GetAllMetadata()
	blobs := make(map[string]bool)
	for id := range expected {
		blobs[files[id].SHA1] = true
	}
	if len(blobs) != 1 {
		t.Error("WHOLE_QUOTA_SHARED accepted identical uploads did not share one blob")
	}
	snapshot, err := uploadquota.Measure(files)
	if err != nil || snapshot.Users[sec030WholeOwner] != (uploadquota.Usage{Bytes: 1536 * 1024, Files: 2}) {
		t.Error("WHOLE_QUOTA_SHARED shared content was not charged for both metadata records")
	}
	database.Close()
	database.Connect(connection)
	sec030WholeCheckFiles(t, expected, nil, nil)
	sec030WholeCheckLedger(t, expected)
	key := sec030WholeKey(t)
	response := sec030WholeSend(context.Background(), client, server.URL, key.Id, form)
	if response.failed || !sec030WholeIsQuota(response) {
		t.Errorf("WHOLE_QUOTA_REOPEN capacity changed after reconnect: status=%d code=%d", response.status, response.ErrorCode)
	}
	sec030WholeCheckFiles(t, expected, nil, nil)
}

func sec030WholeRunChild(t *testing.T, testName, selected, dimension string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("WHOLE_QUOTA_SETUP cannot locate isolated test executable")
	}
	policy := map[string]string{
		sec030WholeChildEnv:                selected,
		"GOKAPI_UPLOAD_USER_TOTAL_MB":      "64",
		"GOKAPI_UPLOAD_USER_MAX_FILES":     "1000",
		"GOKAPI_UPLOAD_USER_ACTIVE":        "64",
		"GOKAPI_UPLOAD_INSTANCE_TOTAL_MB":  "128",
		"GOKAPI_UPLOAD_INSTANCE_MAX_FILES": "10000",
		"GOKAPI_UPLOAD_INSTANCE_ACTIVE":    "64",
		"GOKAPI_UPLOAD_STAGING_MB":         "512",
		"GOKAPI_UPLOAD_BODY_SLOTS":         "64",
		"GOKAPI_UPLOAD_RECEIVE_MB":         "512",
	}
	switch dimension {
	case "bytes":
		policy["GOKAPI_UPLOAD_USER_TOTAL_MB"] = "1"
	case "files":
		policy["GOKAPI_UPLOAD_USER_MAX_FILES"] = "2"
	case "shared":
		policy["GOKAPI_UPLOAD_USER_TOTAL_MB"] = "2"
	}
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := policy[strings.ToUpper(name)]; !replaced {
			environment = append(environment, entry)
		}
	}
	for name, value := range policy {
		environment = append(environment, name+"="+value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^"+testName+"$", "-test.v", "-test.timeout=90s")
	command.Dir = t.TempDir()
	command.Env = environment
	output, err := command.CombinedOutput()
	// Only our bounded numeric diagnostics reach the parent output. Fixture
	// startup messages and HTTP transport errors can contain URLs or secrets.
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "WHOLE_QUOTA_") {
			t.Log(strings.TrimSpace(line))
		}
	}
	if err != nil {
		t.Fatal("WHOLE_QUOTA_CHILD isolated API contract failed")
	}
}

func sec030WholeSetup(t *testing.T, backend string) models.DbConnection {
	t.Helper()
	connection := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "whole.sqlite")}
	if backend == "redis" {
		connection = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "sec030_whole_"}
	} else if backend != "sqlite" {
		t.Fatal("WHOLE_QUOTA_SETUP invalid backend")
	}
	database.Close()
	cfg := configuration.Get()
	saved := *cfg
	t.Cleanup(func() {
		database.Close()
		*cfg = saved
		filesystem.Init(saved.DataDir)
		configuration.ConnectDatabase()
	})
	cfg.DataDir = t.TempDir()
	cfg.MaxFileSizeMB = 4
	cfg.MaxMemory = 1
	cfg.Encryption.Level = encryption.NoEncryption
	filesystem.Init(cfg.DataDir)
	database.Connect(connection)
	database.SaveUser(models.User{Id: sec030WholeOwner, Name: "synthetic-whole-owner", UserLevel: models.UserLevelUser}, false)
	return connection
}

func sec030WholeKey(t *testing.T) models.ApiKey {
	t.Helper()
	key := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: sec030WholeOwner, Permissions: models.ApiPermUpload}
	database.SaveApiKey(key)
	return key
}

func sec030WholeSeed(t *testing.T, content []byte) models.File {
	t.Helper()
	hash := sha1.Sum(content) // Existing blob naming, never password hashing.
	file := models.File{Id: rand.Text(), Name: "synthetic-seed.bin", SHA1: hex.EncodeToString(hash[:]),
		SizeBytes: int64(len(content)), ContentType: "application/octet-stream", UserId: sec030WholeOwner,
		UnlimitedTime: true, UnlimitedDownloads: true}
	if err := os.WriteFile(filepath.Join(configuration.Get().DataDir, file.SHA1), content, 0600); err != nil {
		t.Fatal("WHOLE_QUOTA_SETUP cannot write synthetic seed blob")
	}
	database.SaveMetaData(file)
	return file
}

type sec030WholeForm struct {
	body        []byte
	contentType string
}

func sec030WholeMultipart(t *testing.T, payload []byte) sec030WholeForm {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "synthetic-whole.bin")
	if err != nil {
		t.Fatal("WHOLE_QUOTA_SETUP cannot create synthetic multipart file")
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal("WHOLE_QUOTA_SETUP cannot write synthetic multipart payload")
	}
	for _, field := range [][2]string{{"allowedDownloads", "0"}, {"expiryDays", "0"}} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal("WHOLE_QUOTA_SETUP cannot write synthetic multipart fields")
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal("WHOLE_QUOTA_SETUP cannot finish synthetic multipart")
	}
	return sec030WholeForm{body: body.Bytes(), contentType: writer.FormDataContentType()}
}

type sec030WholeResponse struct {
	Result       string
	FileInfo     models.FileApiOutput
	ErrorCode    int
	ErrorMessage string
	status       int
	failed       bool
}

func sec030WholeSend(ctx context.Context, client *http.Client, serverURL, key string, form sec030WholeForm) sec030WholeResponse {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+"/files/add", bytes.NewReader(form.body))
	if err != nil {
		return sec030WholeResponse{failed: true}
	}
	request.Header.Set("Content-Type", form.contentType)
	request.Header.Set("apikey", key)
	response, err := client.Do(request)
	if err != nil {
		return sec030WholeResponse{failed: true}
	}
	defer response.Body.Close()
	result := sec030WholeResponse{status: response.StatusCode}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err := decoder.Decode(&result); err != nil {
		result.failed = true
	} else if err := decoder.Decode(new(any)); err != io.EOF {
		result.failed = true
	}
	return result
}

func sec030WholeIsQuota(response sec030WholeResponse) bool {
	return !response.failed && response.status == http.StatusRequestEntityTooLarge &&
		response.Result == "error" && response.ErrorCode == sec030WholeQuotaCode && response.ErrorMessage != ""
}

type sec030WholeGateBody struct {
	io.ReadCloser
	ctx     context.Context
	ready   chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (body *sec030WholeGateBody) Read(destination []byte) (int, error) {
	body.once.Do(func() { body.ready <- struct{}{} })
	select {
	case <-body.release:
		return body.ReadCloser.Read(destination)
	case <-body.ctx.Done():
		return 0, body.ctx.Err()
	}
}

func sec030WholeCheckFiles(t *testing.T, expected map[string][]byte, seed *models.File, seedContent []byte) {
	t.Helper()
	files := database.GetAllMetadata()
	wantCount := len(expected)
	if seed != nil {
		wantCount++
		if !reflect.DeepEqual(files[seed.Id], *seed) {
			t.Error("WHOLE_QUOTA_SEED existing metadata changed")
		}
		data, err := os.ReadFile(filepath.Join(configuration.Get().DataDir, seed.SHA1))
		if err != nil || !bytes.Equal(data, seedContent) {
			t.Error("WHOLE_QUOTA_SEED existing blob changed")
		}
	}
	if len(files) != wantCount {
		t.Errorf("WHOLE_QUOTA_METADATA records=%d; want %d", len(files), wantCount)
	}
	for id, content := range expected {
		file, found := files[id]
		if !found || file.UserId != sec030WholeOwner || file.UploadRequestId != "" || file.SizeBytes != int64(len(content)) {
			t.Error("WHOLE_QUOTA_METADATA completed metadata differs from the authenticated upload")
			continue
		}
		data, err := os.ReadFile(filepath.Join(configuration.Get().DataDir, file.SHA1))
		if err != nil || !bytes.Equal(data, content) {
			t.Error("WHOLE_QUOTA_CONTENT accepted metadata does not identify the uploaded bytes")
		}
	}
}

func sec030WholeCheckLedger(t *testing.T, expected map[string][]byte) {
	t.Helper()
	ledger, err := database.ReadUploadQuota(context.Background())
	if err != nil {
		t.Error("WHOLE_QUOTA_LEDGER cannot read durable receipts")
		return
	}
	found := make(map[string]bool)
	for _, record := range ledger.Records {
		if record.UserID != sec030WholeOwner {
			t.Error("WHOLE_QUOTA_LEDGER receipt has an unexpected authenticated owner")
			continue
		}
		if record.State != uploadquota.Committed {
			if record.State == uploadquota.Reserved || record.State == uploadquota.Uploading || !record.StagingReleased {
				t.Error("WHOLE_QUOTA_LEDGER completed requests left outstanding reservations")
			}
			continue
		}
		content, exists := expected[record.FileID]
		if !exists || found[record.FileID] || record.RequestID != "" || record.Bytes != int64(len(content)) || !record.StagingReleased {
			t.Error("WHOLE_QUOTA_LEDGER committed receipt differs from the completed upload")
		}
		found[record.FileID] = true
	}
	if len(found) != len(expected) {
		t.Errorf("WHOLE_QUOTA_LEDGER receipts=%d; want %d", len(found), len(expected))
	}
}
