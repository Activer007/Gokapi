//go:build test && security_whole && !integration

package api

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/chunking"
	"github.com/forceu/gokapi/internal/uploadquota"
)

const sec030ChunkChildEnv = "GOKAPI_SEC030_CHUNK_QUOTA_CHILD"

type sec030ChunkCase struct {
	backend, kind, scope, dimension string
	count                           int
}

func sec030ChunkScopes(kind string) []string {
	if kind == "fr" {
		return []string{"user", "instance", "request"}
	}
	return []string{"user", "instance"}
}

// These requests use existing routes and models only. Baseline failures must
// reach API assertions, rather than depend on a not-yet-created runtime symbol.
func TestSEC030ChunkQuotaAdmission(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, kind := range []string{"ordinary", "fr"} {
				for _, scope := range sec030ChunkScopes(kind) {
					for _, dimension := range []string{"bytes", "files", "active"} {
						for _, count := range []int{10, 50} {
							candidate := sec030ChunkCase{backend, kind, scope, dimension, count}
							t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkQuotaAdmission", candidate) })
						}
					}
				}
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	connection := sec030WholeSetup(t, candidate.backend)
	configuration.Get().ChunkSize = 1
	server, gate := sec030ChunkServer(t, candidate.count)
	client := server.Client()
	client.Timeout = 30 * time.Second
	request, requestKey := sec030ChunkRequestScope(t, candidate)
	if candidate.dimension != "active" {
		content := []byte("seed")
		if candidate.dimension == "bytes" {
			content = bytes.Repeat([]byte{0x73}, 1024*1024-1)
		}
		seed := sec030WholeSeed(t, content)
		if candidate.kind == "fr" {
			seed.UploadRequestId = request.Id
			database.SaveMetaData(seed)
		}
	}
	reserveOnly := candidate.kind == "fr" && candidate.dimension != "bytes"
	requests := make([]sec030ChunkCall, candidate.count)
	for i := range requests {
		if reserveOnly {
			requests[i] = sec030ChunkReserveCall(request, requestKey)
			continue
		}
		key, uuid := requestKey, rand.Text()
		if candidate.kind == "ordinary" {
			key = sec030WholeKey(t)
		} else {
			uuid = sec030ChunkReserveOne(t, client, server.URL, request, requestKey)
		}
		requests[i] = sec030ChunkAddCall(t, candidate.kind, request.Id, key.Id, uuid, 1, 0, []byte{byte(i + 1)})
	}
	for _, call := range requests {
		stored, ok := database.GetApiKey(call.key)
		if !ok || stored.UserId != sec030WholeOwner || stored.PublicId == "" {
			t.Fatal("CHUNK_QUOTA_SETUP all distinct credentials must persist before contention")
		}
	}
	metadata := database.GetAllMetadata()
	responses := sec030ChunkBatch(t, client, server.URL, gate, requests)
	winners, denied := make([]int, 0), make([]int, 0)
	for i, response := range responses {
		if response.failed {
			t.Error("CHUNK_QUOTA_TRANSPORT request or bounded JSON response failed")
			continue
		}
		if response.status == http.StatusUnauthorized || response.ErrorCode == sec030WholeBusyCode {
			t.Errorf("CHUNK_QUOTA_FALSE_PROOF authentication/receiver rejection status=%d code=%d", response.status, response.ErrorCode)
			continue
		}
		if response.status == http.StatusOK && response.Result == "OK" && (!reserveOnly || response.Uuid != "") {
			winners = append(winners, i)
			if reserveOnly {
				requests[i].uuid = response.Uuid
			}
		} else if sec030ChunkDenied(response, candidate.dimension) {
			denied = append(denied, i)
		} else {
			t.Errorf("CHUNK_QUOTA_RESPONSE unexpected status=%d code=%d", response.status, response.ErrorCode)
		}
	}
	if len(winners) != 1 || len(denied) != candidate.count-1 {
		t.Errorf("CHUNK_QUOTA_CAPACITY accepted=%d denied=%d; want 1/%d", len(winners), len(denied), candidate.count-1)
	}
	if !reflect.DeepEqual(metadata, database.GetAllMetadata()) {
		t.Error("CHUNK_QUOTA_METADATA first-chunk/reserve admission changed completed metadata")
	}
	for _, i := range denied {
		if requests[i].uuid != "" && chunking.FileExists(chunking.StorageID(requests[i].uuid, sec030WholeOwner, request.Id)) {
			t.Error("CHUNK_QUOTA_REJECTION rejected admission created a persistent chunk")
		}
	}
	wantRecords := 1
	if candidate.kind == "fr" && candidate.dimension == "bytes" {
		wantRecords = candidate.count // Existing unbound reservations survive a failed Bind.
	}
	wantBound := 1
	if reserveOnly {
		wantBound = 0
	}
	sec030ChunkLedger(t, request.Id, wantRecords, wantBound, int64(wantBound))
	if len(winners) == 1 {
		winner := requests[winners[0]]
		wantPayload := []byte{byte(winners[0] + 1)}
		if reserveOnly {
			wantPayload = []byte{0x61}
			winner = sec030ChunkAddCall(t, "fr", request.Id, requestKey.Id, winner.uuid, 1, 0, []byte{0x61})
			if response := sec030ChunkSend(context.Background(), client, server.URL, winner); !sec030ChunkOK(response) {
				t.Errorf("CHUNK_QUOTA_BIND winning reservation could not bind: status=%d code=%d", response.status, response.ErrorCode)
			}
		}
		before := sec030ChunkSnapshot(t)
		if response := sec030ChunkSend(context.Background(), client, server.URL, winner); !sec030ChunkOK(response) {
			t.Errorf("CHUNK_QUOTA_RETRY same declaration was charged again: status=%d code=%d", response.status, response.ErrorCode)
		}
		sec030ChunkLedger(t, request.Id, wantRecords, 1, 1)
		if !sec030ChunkHasPayload(sec030ChunkPayloads(t), wantPayload) {
			t.Error("CHUNK_QUOTA_CONTENT admitted upload did not retain its exact payload")
		}
		if !reflect.DeepEqual(before.payloads, sec030ChunkPayloads(t)) {
			t.Error("CHUNK_QUOTA_RETRY identical retry changed persistent payloads")
		}
	}
	if len(denied) > 0 {
		sec030ChunkRejectUnchanged(t, client, server.URL, requests[denied[0]], candidate.dimension)
	}
	beforeReconnect := sec030ChunkSnapshot(t)
	database.Close()
	database.Connect(connection)
	if !reflect.DeepEqual(beforeReconnect, sec030ChunkSnapshot(t)) {
		t.Error("CHUNK_QUOTA_REOPEN reconnect lost reservations, metadata or payload")
	}
	if len(denied) > 0 {
		sec030ChunkRejectUnchanged(t, client, server.URL, requests[denied[0]], candidate.dimension)
	}
}

// A tiny first-arriving tail still declares the entire logical file. Admission
// must not charge just its one received byte or preallocate before refusal.
func TestSEC030ChunkDeclaredCapacity(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, kind := range []string{"ordinary", "fr"} {
				for _, scope := range sec030ChunkScopes(kind) {
					candidate := sec030ChunkCase{backend, kind, scope, "declared", 1}
					t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkDeclaredCapacity", candidate) })
				}
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	sec030WholeSetup(t, candidate.backend)
	configuration.Get().ChunkSize = 1
	request, key := sec030ChunkRequestScope(t, candidate)
	seed := sec030WholeSeed(t, bytes.Repeat([]byte{0x73}, 1024*1024-1))
	if candidate.kind == "fr" {
		seed.UploadRequestId = request.Id
		database.SaveMetaData(seed)
	} else {
		key = sec030WholeKey(t)
	}
	server, _ := sec030ChunkServer(t, 1)
	client := server.Client()
	client.Timeout = 30 * time.Second
	uuid := rand.Text()
	if candidate.kind == "fr" {
		uuid = sec030ChunkReserveOne(t, client, server.URL, request, key)
	}
	call := sec030ChunkAddCall(t, candidate.kind, request.Id, key.Id, uuid, 1024*1024, 1024*1024-1, []byte{0x61})
	sec030ChunkRejectUnchanged(t, client, server.URL, call, "bytes")
}

func TestSEC030ChunkRequestCumulative(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			candidate := sec030ChunkCase{backend, "fr", "request", "cumulative", 1}
			t.Run(backend, func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkRequestCumulative", candidate) })
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	connection := sec030WholeSetup(t, candidate.backend)
	request, key := sec030ChunkRequestScope(t, candidate)
	server, _ := sec030ChunkServer(t, 1)
	client := server.Client()
	client.Timeout = 30 * time.Second
	first := sec030ChunkReserveOne(t, client, server.URL, request, key)
	second := sec030ChunkReserveOne(t, client, server.URL, request, key)
	payload := bytes.Repeat([]byte{0x61}, 768*1024)
	call := sec030ChunkAddCall(t, "fr", request.Id, key.Id, first, int64(len(payload)), 0, payload)
	if response := sec030ChunkSend(context.Background(), client, server.URL, call); !sec030ChunkOK(response) {
		t.Fatalf("CHUNK_QUOTA_SINGLE_FILE first 768KiB upload at MaxSize1MiB failed: status=%d code=%d", response.status, response.ErrorCode)
	}
	sec030ChunkLedger(t, request.Id, 2, 1, int64(len(payload)))
	before := sec030ChunkSnapshot(t)
	if response := sec030ChunkSend(context.Background(), client, server.URL, call); !sec030ChunkOK(response) {
		t.Errorf("CHUNK_QUOTA_RETRY repeated first upload failed: status=%d code=%d", response.status, response.ErrorCode)
	}
	sec030ChunkLedger(t, request.Id, 2, 1, int64(len(payload)))
	if !reflect.DeepEqual(before.payloads, sec030ChunkPayloads(t)) {
		t.Error("CHUNK_QUOTA_RETRY repeated first upload changed stored payloads")
	}
	secondCall := sec030ChunkAddCall(t, "fr", request.Id, key.Id, second, int64(len(payload)), 0, payload)
	sec030ChunkRejectUnchanged(t, client, server.URL, secondCall, "bytes")
	if !sec030ChunkHasPayload(sec030ChunkPayloads(t), payload) {
		t.Error("CHUNK_QUOTA_CONTENT refusal changed the first request-owned chunk")
	}
	before = sec030ChunkSnapshot(t)
	database.Close()
	database.Connect(connection)
	if !reflect.DeepEqual(before, sec030ChunkSnapshot(t)) {
		t.Error("CHUNK_QUOTA_REOPEN request usage did not survive reconnect")
	}
	sec030ChunkRejectUnchanged(t, client, server.URL, secondCall, "bytes")
}

func (candidate sec030ChunkCase) name() string {
	return fmt.Sprintf("%s/%s/%s/%s/%d", candidate.backend, candidate.kind, candidate.scope, candidate.dimension, candidate.count)
}

func sec030ChunkSelected(t *testing.T) sec030ChunkCase {
	t.Helper()
	parts := strings.Split(os.Getenv(sec030ChunkChildEnv), "/")
	if len(parts) != 5 {
		t.Fatal("CHUNK_QUOTA_SETUP invalid isolated case")
	}
	count, err := strconv.Atoi(parts[4])
	if err != nil || count < 1 || count > 50 {
		t.Fatal("CHUNK_QUOTA_SETUP invalid contender count")
	}
	return sec030ChunkCase{parts[0], parts[1], parts[2], parts[3], count}
}

func sec030ChunkRunChild(t *testing.T, testName string, candidate sec030ChunkCase) {
	t.Helper()
	policy := map[string]string{
		sec030ChunkChildEnv:           candidate.name(),
		"GOKAPI_UPLOAD_USER_TOTAL_MB": "64", "GOKAPI_UPLOAD_USER_MAX_FILES": "1000", "GOKAPI_UPLOAD_USER_ACTIVE": "64",
		"GOKAPI_UPLOAD_INSTANCE_TOTAL_MB": "128", "GOKAPI_UPLOAD_INSTANCE_MAX_FILES": "10000", "GOKAPI_UPLOAD_INSTANCE_ACTIVE": "64",
		"GOKAPI_UPLOAD_REQUEST_TOTAL_MB": "64", "GOKAPI_UPLOAD_REQUEST_ACTIVE": "64",
		"GOKAPI_UPLOAD_BODY_SLOTS": "64", "GOKAPI_UPLOAD_RECEIVE_MB": "512", "GOKAPI_UPLOAD_STAGING_MB": "512",
	}
	prefix := "GOKAPI_UPLOAD_" + strings.ToUpper(candidate.scope)
	switch candidate.dimension {
	case "bytes", "declared", "cumulative":
		policy[prefix+"_TOTAL_MB"] = "1"
	case "files":
		if candidate.scope != "request" {
			policy[prefix+"_MAX_FILES"] = "2"
		}
	case "active":
		policy[prefix+"_ACTIVE"] = "1"
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
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("CHUNK_QUOTA_SETUP cannot locate isolated executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^"+testName+"$", "-test.v", "-test.timeout=80s")
	command.Dir, command.Env = t.TempDir(), environment
	output, err := command.CombinedOutput()
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "CHUNK_QUOTA_") {
			t.Log(strings.TrimSpace(line))
		}
	}
	if err != nil {
		t.Fatal("CHUNK_QUOTA_CHILD isolated API contract failed")
	}
}

func sec030ChunkRequestScope(t *testing.T, candidate sec030ChunkCase) (models.FileRequest, models.ApiKey) {
	t.Helper()
	if candidate.kind != "fr" {
		return models.FileRequest{}, models.ApiKey{}
	}
	owner, ok := database.GetUser(sec030WholeOwner)
	if !ok {
		t.Fatal("CHUNK_QUOTA_SETUP missing synthetic owner")
	}
	owner.Permissions |= models.UserPermGuestUploads
	database.SaveUser(owner, false)
	key := sec030WholeKey(t)
	request := models.FileRequest{Id: rand.Text(), UserId: owner.Id, ApiKey: key.Id, MaxFiles: 1000, MaxSize: 1, Expiry: time.Now().Add(time.Hour).Unix()}
	if candidate.scope == "request" && candidate.dimension == "files" {
		request.MaxFiles = 2
	}
	key.UploadRequestId = request.Id
	database.SaveApiKey(key)
	database.SaveFileRequest(request)
	return request, key
}

type sec030ChunkCall struct {
	path, key, uuid string
	headers         map[string]string
	form            sec030WholeForm
}

type sec030ChunkResponse struct {
	sec030WholeResponse
	Uuid string
}

func sec030ChunkReserveCall(request models.FileRequest, key models.ApiKey) sec030ChunkCall {
	return sec030ChunkCall{path: "/uploadrequest/chunk/reserve", key: key.Id, headers: map[string]string{"id": request.Id}}
}

func sec030ChunkReserveOne(t *testing.T, client *http.Client, serverURL string, request models.FileRequest, key models.ApiKey) string {
	t.Helper()
	response := sec030ChunkSend(context.Background(), client, serverURL, sec030ChunkReserveCall(request, key))
	if !sec030ChunkOK(response) || response.Uuid == "" {
		t.Fatalf("CHUNK_QUOTA_SETUP valid unbound reservation failed: status=%d code=%d", response.status, response.ErrorCode)
	}
	return response.Uuid
}

func sec030ChunkAddCall(t *testing.T, kind, requestID, key, uuid string, total, offset int64, payload []byte) sec030ChunkCall {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "synthetic-chunk.bin")
	if err != nil {
		t.Fatal("CHUNK_QUOTA_SETUP multipart file failed")
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal("CHUNK_QUOTA_SETUP multipart body failed")
	}
	for _, field := range [][2]string{{"uuid", uuid}, {"filesize", strconv.FormatInt(total, 10)}, {"offset", strconv.FormatInt(offset, 10)}} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal("CHUNK_QUOTA_SETUP multipart fields failed")
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal("CHUNK_QUOTA_SETUP multipart close failed")
	}
	call := sec030ChunkCall{path: "/chunk/add", key: key, uuid: uuid, form: sec030WholeForm{body: body.Bytes(), contentType: writer.FormDataContentType()}}
	if kind == "fr" {
		call.path, call.headers = "/uploadrequest/chunk/add", map[string]string{"fileRequestId": requestID}
	}
	return call
}

func sec030ChunkSend(ctx context.Context, client *http.Client, serverURL string, call sec030ChunkCall) sec030ChunkResponse {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL+call.path, bytes.NewReader(call.form.body))
	if err != nil {
		return sec030ChunkResponse{sec030WholeResponse: sec030WholeResponse{failed: true}}
	}
	request.Header.Set("apikey", call.key)
	if call.form.contentType != "" {
		request.Header.Set("Content-Type", call.form.contentType)
	}
	for name, value := range call.headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return sec030ChunkResponse{sec030WholeResponse: sec030WholeResponse{failed: true}}
	}
	defer response.Body.Close()
	result := sec030ChunkResponse{sec030WholeResponse: sec030WholeResponse{status: response.StatusCode}}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64*1024))
	if err := decoder.Decode(&result); err != nil {
		result.failed = true
	} else if err := decoder.Decode(new(any)); err != io.EOF {
		result.failed = true
	}
	return result
}

func sec030ChunkOK(response sec030ChunkResponse) bool {
	return !response.failed && response.status == http.StatusOK && response.Result == "OK"
}

func sec030ChunkDenied(response sec030ChunkResponse, dimension string) bool {
	status, code := http.StatusRequestEntityTooLarge, 21
	if dimension == "active" {
		status, code = http.StatusTooManyRequests, 22
	}
	return !response.failed && response.status == status && response.ErrorCode == code && response.Result == "error" && response.ErrorMessage != ""
}

type sec030ChunkGate struct {
	enabled atomic.Bool
	arrived chan struct{}
	release chan struct{}
	once    sync.Once
}

func sec030ChunkServer(t *testing.T, capacity int) (*httptest.Server, *sec030ChunkGate) {
	t.Helper()
	gate := &sec030ChunkGate{arrived: make(chan struct{}, capacity), release: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gate.enabled.Load() {
			gate.arrived <- struct{}{}
			select {
			case <-gate.release:
			case <-r.Context().Done():
				return
			}
		}
		Process(w, r)
	}))
	t.Cleanup(func() { gate.once.Do(func() { close(gate.release) }); server.Close() })
	return server, gate
}

func sec030ChunkBatch(t *testing.T, client *http.Client, serverURL string, gate *sec030ChunkGate, calls []sec030ChunkCall) []sec030ChunkResponse {
	t.Helper()
	gate.enabled.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	responses := make([]sec030ChunkResponse, len(calls))
	var workers sync.WaitGroup
	for i := range calls {
		workers.Add(1)
		go func(i int) { defer workers.Done(); responses[i] = sec030ChunkSend(ctx, client, serverURL, calls[i]) }(i)
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	arrived := 0
waitForRequests:
	for arrived < len(calls) {
		select {
		case <-gate.arrived:
			arrived++
		case <-timer.C:
			break waitForRequests
		}
	}
	gate.once.Do(func() { close(gate.release) })
	gate.enabled.Store(false)
	workers.Wait()
	if arrived != len(calls) {
		t.Errorf("CHUNK_QUOTA_BARRIER handler arrivals=%d; want %d", arrived, len(calls))
	}
	return responses
}

type sec030ChunkState struct {
	ledger   uploadquota.Ledger
	metadata map[string]models.File
	payloads map[string][]byte
}

func sec030ChunkSnapshot(t *testing.T) sec030ChunkState {
	t.Helper()
	ledger, err := database.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal("CHUNK_QUOTA_LEDGER durable state could not be read")
	}
	return sec030ChunkState{ledger, database.GetAllMetadata(), sec030ChunkPayloads(t)}
}

func sec030ChunkPayloads(t *testing.T) map[string][]byte {
	t.Helper()
	root := configuration.Get().DataDir
	const maxEntries = 512
	const maxFileBytes = 4 * 1024 * 1024
	const maxTotalBytes = 64 * 1024 * 1024
	result := make(map[string][]byte)
	entriesSeen, bytesSeen := 0, 0
	var visit func(string, string)
	visit = func(path, relative string) {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("CHUNK_QUOTA_CONTENT missing or symlinked snapshot entry")
		}
		if info.IsDir() {
			// Root creation is harmless bootstrap state. Owned session
			// directories below it are recorded, including empty directories.
			if relative != "" && relative != ".gokapi-chunks-v1" && relative != ".gokapi-receive-v1" {
				result[relative+"/"] = nil
			}
			directory, err := os.Open(path)
			if err != nil {
				t.Fatal("CHUNK_QUOTA_CONTENT cannot open snapshot directory")
			}
			entries, readErr := directory.ReadDir(maxEntries - entriesSeen + 1)
			closeErr := directory.Close()
			if (readErr != nil && readErr != io.EOF) || closeErr != nil || entriesSeen+len(entries) > maxEntries {
				t.Fatal("CHUNK_QUOTA_CONTENT snapshot exceeds entry bound or cannot be read")
			}
			entriesSeen += len(entries)
			if relative == ".gokapi-receive-v1" && (len(entries) != 1 || entries[0].Name() != ".owner") {
				t.Error("CHUNK_QUOTA_RECEPTION receive spool must retain only its ownership marker")
			}
			for _, entry := range entries {
				if relative == ".gokapi-receive-v1" && entry.Name() != ".owner" {
					t.Error("CHUNK_QUOTA_RECEPTION receive payload was not cleaned")
				}
				visit(filepath.Join(path, entry.Name()), filepath.ToSlash(filepath.Join(relative, entry.Name())))
			}
			return
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxFileBytes {
			t.Fatal("CHUNK_QUOTA_CONTENT snapshot contains an unsupported or oversized file")
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal("CHUNK_QUOTA_CONTENT cannot open isolated payload or manifest")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > maxFileBytes || bytesSeen > maxTotalBytes-len(data) {
			t.Fatal("CHUNK_QUOTA_CONTENT snapshot exceeds byte bounds or cannot be read")
		}
		bytesSeen += len(data)
		// Only the two exact root ownership markers are ignored. Every
		// manifest, payload and nested marker participates in comparisons.
		if relative != ".gokapi-chunks-v1/.owner" && relative != ".gokapi-receive-v1/.owner" {
			result[relative] = data
		}
	}
	visit(root, "")
	return result
}

func sec030ChunkHasPayload(snapshot map[string][]byte, expected []byte) bool {
	for _, data := range snapshot {
		if bytes.Equal(data, expected) {
			return true
		}
	}
	return false
}

func sec030ChunkRejectUnchanged(t *testing.T, client *http.Client, serverURL string, call sec030ChunkCall, dimension string) {
	t.Helper()
	before := sec030ChunkSnapshot(t)
	response := sec030ChunkSend(context.Background(), client, serverURL, call)
	if !sec030ChunkDenied(response, dimension) {
		t.Errorf("CHUNK_QUOTA_REJECTION status=%d code=%d did not match permanent/active quota contract", response.status, response.ErrorCode)
	}
	if !reflect.DeepEqual(before, sec030ChunkSnapshot(t)) {
		t.Error("CHUNK_QUOTA_REJECTION rejected request changed payload, ledger or completed metadata")
	}
}

func sec030ChunkLedger(t *testing.T, requestID string, records, bound int, total int64) {
	t.Helper()
	ledger, err := database.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal("CHUNK_QUOTA_LEDGER cannot inspect durable reservations")
	}
	known := 0
	var bytes int64
	for _, record := range ledger.Records {
		if record.UserID != sec030WholeOwner || record.RequestID != requestID || record.StagingReleased || (record.State != uploadquota.Reserved && record.State != uploadquota.Uploading) {
			t.Error("CHUNK_QUOTA_LEDGER active reservation lost authenticated scope or pending charge")
		}
		if record.BytesKnown {
			known++
			bytes += record.Bytes
		}
	}
	if len(ledger.Records) != records || known != bound || bytes != total {
		t.Errorf("CHUNK_QUOTA_LEDGER records=%d bound=%d bytes=%d; want %d/%d/%d", len(ledger.Records), known, bytes, records, bound, total)
	}
}
